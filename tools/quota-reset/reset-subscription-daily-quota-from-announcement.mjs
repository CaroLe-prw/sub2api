#!/usr/bin/env node

import fs from 'node:fs/promises';
import path from 'node:path';
import { spawn } from 'node:child_process';

const DEFAULT_STATE_FILE = '/var/lib/sub2api/subscription-daily-quota-reset-state.json';
const MANAGED_BLOCK_START = '<!-- sub2api-quota-reset:start -->';
const MANAGED_BLOCK_END = '<!-- sub2api-quota-reset:end -->';

let config;

main().catch((error) => {
  console.error(`[quota-reset] failed: ${error.message}`);
  process.exitCode = 1;
});

async function main() {
  config = buildConfig();

  if (config.intervalHours <= 0) {
    throw new Error('RESET_INTERVAL_HOURS must be greater than 0');
  }
  if (config.pageSize <= 0) {
    throw new Error('PAGE_SIZE must be greater than 0');
  }
  if (config.concurrency <= 0) {
    throw new Error('RESET_CONCURRENCY must be greater than 0');
  }
  if (!['update', 'create'].includes(config.announcementPublishMode)) {
    throw new Error('ANNOUNCEMENT_PUBLISH_MODE must be update or create');
  }

  const now = new Date();
  const state = await readState(config.stateFile);
  const lastRunAt = parseDate(state.last_success_at);
  const nextRunAt = lastRunAt ? addHours(lastRunAt, config.intervalHours) : null;

  const announcement = await apiGet(`/admin/announcements/${config.announcementID}`);
  const groupIDs = extractGroupIDs(announcement.targeting);
  if (groupIDs.length === 0) {
    throw new Error(`announcement ${config.announcementID} has no targeting group_ids; refusing to reset all subscriptions`);
  }
  const groupNames = await resolveGroupNames(groupIDs);

  if (config.noticeOnly) {
    const stateLastRunAt = parseDate(state.last_success_at);
    const stateNextRunAt = parseDate(state.next_run_at) || (stateLastRunAt ? addHours(stateLastRunAt, config.intervalHours) : null);
    if (!stateLastRunAt) {
      throw new Error('NOTICE_ONLY requires an existing state file with last_success_at');
    }
    if (config.patchDailyWindowStart && !config.dryRun) {
      const subscriptions = await listActiveSubscriptionsByGroups(groupIDs);
      const resettableSubscriptions = subscriptions.filter(hasAllQuotaWindowStarts);
      await patchDailyWindowStart(
        resettableSubscriptions.map((sub) => sub.id),
        stateLastRunAt,
        { resetUsage: false },
      );
    }
    if (config.updateAnnouncement && !config.dryRun) {
      await publishAnnouncementNotice(announcement, {
        status: 'success',
        lastRunAt: stateLastRunAt,
        nextRunAt: stateNextRunAt,
        groupIDs,
        groupNames,
        resetCount: Number(state.last_reset_count || 0),
        failedCount: 0,
      });
    }
    console.log('[quota-reset] notice-only done');
    return;
  }

  if (config.enforceInterval && nextRunAt && now < nextRunAt) {
    console.log(`[quota-reset] skip: last successful reset was ${lastRunAt.toISOString()}, next reset is ${nextRunAt.toISOString()}`);
    if (config.updateAnnouncement && !config.dryRun) {
      await publishAnnouncementNotice(announcement, {
        status: 'waiting',
        lastRunAt,
        nextRunAt,
        groupIDs,
        groupNames,
        resetCount: Number(state.last_reset_count || 0),
        failedCount: 0,
      });
    }
    return;
  }

  console.log(`[quota-reset] announcement=${config.announcementID} groups=${groupIDs.join(',')}`);
  const subscriptions = await listActiveSubscriptionsByGroups(groupIDs);
  const resettableSubscriptions = subscriptions.filter(hasAllQuotaWindowStarts);
  const skippedSubscriptions = subscriptions.filter((sub) => !hasAllQuotaWindowStarts(sub));
  console.log(
    `[quota-reset] active subscriptions=${subscriptions.length} resettable=${resettableSubscriptions.length} skipped_missing_windows=${skippedSubscriptions.length}`,
  );

  if (config.dryRun) {
    for (const sub of resettableSubscriptions) {
      console.log(`[quota-reset] dry-run subscription=${sub.id} user=${sub.user?.email || sub.user_id || 'unknown'} group=${sub.group_id}`);
    }
    for (const sub of skippedSubscriptions) {
      console.log(
        `[quota-reset] dry-run skip subscription=${sub.id} missing_windows=${missingQuotaWindowNames(sub).join(',')}`,
      );
    }
    return;
  }

  for (const sub of skippedSubscriptions) {
    console.log(
      `[quota-reset] skip subscription=${sub.id} missing_windows=${missingQuotaWindowNames(sub).join(',')}`,
    );
  }

  const results = await runLimited(resettableSubscriptions, config.concurrency, async (sub) => {
    await resetSubscriptionDailyQuota(sub.id);
    return sub.id;
  });

  const failed = results.filter((item) => item.status === 'rejected');
  const succeeded = results.filter((item) => item.status === 'fulfilled');
  const runFinishedAt = new Date();
  const followingRunAt = addHours(runFinishedAt, config.intervalHours);
  let dailyWindowStartPatch = null;

  if (config.patchDailyWindowStart && succeeded.length > 0) {
    try {
      await patchDailyWindowStart(
        succeeded.map((item) => item.value),
        runFinishedAt,
        { resetUsage: true },
      );
      dailyWindowStartPatch = {
        ok: true,
        patched_at: runFinishedAt.toISOString(),
        count: succeeded.length,
      };
    } catch (error) {
      dailyWindowStartPatch = {
        ok: false,
        error: error.message,
      };
      console.error(`[quota-reset] daily window start patch failed: ${error.message}`);
    }
  }

  if (failed.length > 0) {
    if (config.updateAnnouncement) {
      await publishAnnouncementNotice(announcement, {
        status: 'partial_failed',
        lastRunAt: runFinishedAt,
        nextRunAt: followingRunAt,
        groupIDs,
        groupNames,
        resetCount: succeeded.length,
        failedCount: failed.length,
      });
    }
    for (const item of failed) {
      console.error(`[quota-reset] reset failed: ${item.reason?.message || item.reason}`);
    }
    throw new Error(`reset finished with ${failed.length} failed subscription(s), succeeded=${succeeded.length}`);
  }

  await writeState(config.stateFile, {
    last_success_at: runFinishedAt.toISOString(),
    next_run_at: followingRunAt.toISOString(),
    announcement_id: config.announcementID,
    group_ids: groupIDs,
    group_names: groupNames,
    last_reset_count: succeeded.length,
    daily_window_start_patch: dailyWindowStartPatch,
  });

  if (config.updateAnnouncement) {
    await publishAnnouncementNotice(announcement, {
      status: 'success',
      lastRunAt: runFinishedAt,
      nextRunAt: followingRunAt,
      groupIDs,
      groupNames,
      resetCount: succeeded.length,
      failedCount: 0,
    });
  }

  if (dailyWindowStartPatch && !dailyWindowStartPatch.ok && config.patchDailyWindowStartStrict) {
    throw new Error('daily quota was reset, but daily_window_start patch failed; check database settings');
  }

  console.log(`[quota-reset] done: reset=${succeeded.length}, next=${followingRunAt.toISOString()}`);
}

function buildConfig() {
  return {
    baseURL: normalizeBaseURL(requiredEnv('SUB2API_BASE_URL')),
    adminAPIKey: requiredEnv('SUB2API_ADMIN_API_KEY'),
    announcementID: intEnv('ANNOUNCEMENT_ID', 2),
    intervalHours: numberEnv('RESET_INTERVAL_HOURS', 5),
    pageSize: intEnv('PAGE_SIZE', 500),
    concurrency: intEnv('RESET_CONCURRENCY', 5),
    stateFile: process.env.STATE_FILE || DEFAULT_STATE_FILE,
    timeZone: process.env.TZ || process.env.DISPLAY_TIME_ZONE || 'Asia/Shanghai',
    dryRun: boolEnv('DRY_RUN', false),
    enforceInterval: boolEnv('ENFORCE_INTERVAL', true),
    updateAnnouncement: boolEnv('UPDATE_ANNOUNCEMENT', true),
    announcementPublishMode: process.env.ANNOUNCEMENT_PUBLISH_MODE || 'update',
    createdAnnouncementTitle: process.env.CREATED_ANNOUNCEMENT_TITLE || '',
    createdAnnouncementNotifyMode: process.env.CREATED_ANNOUNCEMENT_NOTIFY_MODE || '',
    quotaResetLabel: process.env.QUOTA_RESET_LABEL || '',
    noticeOnly: boolEnv('NOTICE_ONLY', false),
    patchDailyWindowStart: boolEnv('PATCH_DAILY_WINDOW_START', false),
    patchDailyWindowStartStrict: boolEnv('PATCH_DAILY_WINDOW_START_STRICT', true),
    databaseURL: process.env.DATABASE_URL || '',
    databaseHost: process.env.DATABASE_HOST || process.env.POSTGRES_HOST || 'postgres',
    databasePort: process.env.DATABASE_PORT || process.env.POSTGRES_PORT || '5432',
    databaseUser: process.env.DATABASE_USER || process.env.POSTGRES_USER || 'sub2api',
    databasePassword: process.env.DATABASE_PASSWORD || process.env.POSTGRES_PASSWORD || '',
    databaseName: process.env.DATABASE_DBNAME || process.env.POSTGRES_DB || 'sub2api',
    databaseSSLMode: process.env.DATABASE_SSLMODE || 'disable',
  };
}

async function listActiveSubscriptionsByGroups(groupIDs) {
  const byID = new Map();
  for (const groupID of groupIDs) {
    let page = 1;
    while (true) {
      const query = new URLSearchParams({
        status: 'active',
        group_id: String(groupID),
        page: String(page),
        page_size: String(config.pageSize),
      });
      const data = await apiGet(`/admin/subscriptions?${query.toString()}`);
      const items = Array.isArray(data.items) ? data.items : [];
      for (const item of items) {
        if (item && Number.isInteger(Number(item.id))) {
          byID.set(Number(item.id), item);
        }
      }

      const pages = Number(data.pages || 1);
      if (page >= pages) break;
      page += 1;
    }
  }
  return [...byID.values()].sort((a, b) => Number(a.id) - Number(b.id));
}

async function resetSubscriptionDailyQuota(subscriptionID) {
  await apiPost(`/admin/subscriptions/${subscriptionID}/reset-quota`, {
    daily: true,
    weekly: false,
    monthly: false,
  });
}

function hasAllQuotaWindowStarts(subscription) {
  return missingQuotaWindowNames(subscription).length === 0;
}

function missingQuotaWindowNames(subscription) {
  return [
    ['daily', subscription?.daily_window_start],
    ['weekly', subscription?.weekly_window_start],
    ['monthly', subscription?.monthly_window_start],
  ]
    .filter(([, value]) => value === null || value === undefined || value === '')
    .map(([name]) => name);
}

async function patchDailyWindowStart(subscriptionIDs, windowStart, options = {}) {
  const ids = subscriptionIDs
    .map((id) => Number(id))
    .filter((id) => Number.isInteger(id) && id > 0);
  if (ids.length === 0) {
    return;
  }

  const setClause = options.resetUsage
    ? "daily_usage_usd = 0, daily_window_start = TIMESTAMPTZ '" + windowStart.toISOString() + "', updated_at = NOW()"
    : "daily_window_start = TIMESTAMPTZ '" + windowStart.toISOString() + "', updated_at = NOW()";
  const sql = [
    'UPDATE user_subscriptions',
    `SET ${setClause}`,
    `WHERE id IN (${ids.join(',')});`,
  ].join(' ');

  await runPsql(sql);
  console.log(`[quota-reset] daily_window_start patched=${ids.length} value=${windowStart.toISOString()}`);
}

async function runPsql(sql) {
  const args = buildPsqlArgs(sql);
  const env = {
    ...process.env,
    PGSSLMODE: config.databaseSSLMode,
  };
  if (config.databasePassword) {
    env.PGPASSWORD = config.databasePassword;
  }

  await runCommand('psql', args, env);
}

function buildPsqlArgs(sql) {
  const common = ['-v', 'ON_ERROR_STOP=1', '-X', '-q', '-c', sql];
  if (config.databaseURL) {
    return [config.databaseURL, ...common];
  }
  return [
    '-h',
    config.databaseHost,
    '-p',
    String(config.databasePort),
    '-U',
    config.databaseUser,
    '-d',
    config.databaseName,
    ...common,
  ];
}

async function runCommand(command, args, env) {
  await new Promise((resolve, reject) => {
    const child = spawn(command, args, { env });
    let stdout = '';
    let stderr = '';

    child.stdout.on('data', (chunk) => {
      stdout += chunk.toString();
    });
    child.stderr.on('data', (chunk) => {
      stderr += chunk.toString();
    });
    child.on('error', reject);
    child.on('close', (code) => {
      if (code === 0) {
        resolve();
        return;
      }
      reject(new Error(`${command} exited with ${code}: ${stderr || stdout}`.trim()));
    });
  });
}

async function publishAnnouncementNotice(announcement, summary) {
  if (config.announcementPublishMode === 'create') {
    if (summary.status === 'waiting') {
      return;
    }
    await createAnnouncementNotice(announcement, summary);
    return;
  }

  await updateAnnouncementBlock(announcement, summary);
}

async function updateAnnouncementBlock(announcement, summary) {
  const nextContent = replaceManagedBlock(
    announcement.content || '',
    buildManagedBlock(summary),
  );
  if (nextContent === announcement.content) {
    return;
  }
  await apiPut(`/admin/announcements/${config.announcementID}`, {
    content: nextContent,
  });
  console.log(`[quota-reset] announcement ${config.announcementID} updated`);
}

async function createAnnouncementNotice(sourceAnnouncement, summary) {
  const payload = {
    title: buildCreatedAnnouncementTitle(summary),
    content: buildManagedBlock(summary),
    status: 'active',
    notify_mode: normalizeNotifyMode(config.createdAnnouncementNotifyMode || sourceAnnouncement.notify_mode || 'popup'),
    targeting: sourceAnnouncement.targeting,
  };

  if (summary.lastRunAt) {
    payload.starts_at = Math.floor(summary.lastRunAt.getTime() / 1000);
  }
  if (summary.nextRunAt) {
    payload.ends_at = Math.floor(summary.nextRunAt.getTime() / 1000);
  }

  const created = await apiPost('/admin/announcements', payload);
  console.log(`[quota-reset] announcement notice created id=${created.id || 'unknown'}`);
}

function buildCreatedAnnouncementTitle(summary) {
  if (config.createdAnnouncementTitle.trim() !== '') {
    return config.createdAnnouncementTitle.trim();
  }
  const label = buildQuotaResetLabel(summary, { forTitle: true });
  if (summary.status === 'partial_failed') {
    return `${label}重置部分失败`;
  }
  return `${label}已重置`;
}

function buildManagedBlock(summary) {
  const statusText = {
    success: '已重置',
    waiting: '等待下次重置',
    partial_failed: '部分失败',
  }[summary.status] || summary.status;
  const groupText = formatGroupNames(summary);
  const resetLabel = buildQuotaResetLabel(summary);
  const resetResultText = buildResetResultText(summary);

  const lines = [
    MANAGED_BLOCK_START,
    `### ${resetLabel}重置通知`,
    '',
    `- 当前状态：${statusText}`,
    `- 本次重置范围：${resetLabel}`,
    `- 上次重置：${summary.lastRunAt ? formatForDisplay(summary.lastRunAt) : '暂无记录'}`,
    `- 下次预计重置：${summary.nextRunAt ? formatForDisplay(summary.nextRunAt) : '等待首次执行'}`,
    `- 生效套餐组：${groupText}`,
    `- 处理结果：${resetResultText}`,
    '',
    MANAGED_BLOCK_END,
  ];
  return lines.join('\n');
}

function buildResetResultText(summary) {
  if (summary.failedCount <= 0) {
    return '重置成功';
  }
  return summary.resetCount > 0 ? '部分失败' : '重置失败';
}

function buildQuotaResetLabel(_summary, options = {}) {
  const configured = config.quotaResetLabel.trim();
  if (configured !== '') {
    return configured;
  }
  const hours = formatHours(config.intervalHours);
  return options.forTitle ? `${hours}配额` : `${hours}配额（日限额）`;
}

function formatHours(hours) {
  if (Number.isInteger(hours)) {
    return `${hours}小时`;
  }
  return `${hours}小时`;
}

function formatGroupNames(summary) {
  if (Array.isArray(summary.groupNames) && summary.groupNames.length > 0) {
    return summary.groupNames.join(', ');
  }
  return summary.groupIDs.join(', ');
}

function replaceManagedBlock(content, block) {
  const start = content.indexOf(MANAGED_BLOCK_START);
  const end = content.indexOf(MANAGED_BLOCK_END);
  if (start !== -1 && end !== -1 && end > start) {
    return `${content.slice(0, start).trimEnd()}\n\n${block}\n\n${content.slice(end + MANAGED_BLOCK_END.length).trimStart()}`.trim();
  }
  return `${content.trimEnd()}\n\n${block}`.trim();
}

function extractGroupIDs(value) {
  const ids = new Set();
  walk(value);
  return [...ids].sort((a, b) => a - b);

  function walk(node) {
    if (Array.isArray(node)) {
      for (const item of node) walk(item);
      return;
    }
    if (!node || typeof node !== 'object') {
      return;
    }
    for (const [key, val] of Object.entries(node)) {
      if ((key === 'group_ids' || key === 'groupIds') && Array.isArray(val)) {
        for (const raw of val) {
          const id = Number(raw);
          if (Number.isInteger(id) && id > 0) {
            ids.add(id);
          }
        }
      } else {
        walk(val);
      }
    }
  }
}

async function resolveGroupNames(groupIDs) {
  try {
    const data = await apiGet('/admin/groups/all?include_inactive=true');
    const groups = Array.isArray(data) ? data : Array.isArray(data.items) ? data.items : [];
    const byID = new Map();
    for (const group of groups) {
      const id = Number(group?.id);
      const name = String(group?.name || '').trim();
      if (Number.isInteger(id) && id > 0 && name !== '') {
        byID.set(id, name);
      }
    }
    return groupIDs.map((id) => byID.get(id) || `未知分组 ${id}`);
  } catch (error) {
    console.warn(`[quota-reset] failed to load group names: ${error.message}`);
    return groupIDs.map((id) => `未知分组 ${id}`);
  }
}

async function apiGet(pathname) {
  return apiRequest('GET', pathname);
}

async function apiPost(pathname, body) {
  return apiRequest('POST', pathname, body);
}

async function apiPut(pathname, body) {
  return apiRequest('PUT', pathname, body);
}

async function apiRequest(method, pathname, body) {
  const response = await fetch(`${config.baseURL}${pathname}`, {
    method,
    headers: {
      accept: 'application/json',
      'content-type': 'application/json',
      'x-api-key': config.adminAPIKey,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
  });

  const text = await response.text();
  let payload;
  try {
    payload = text ? JSON.parse(text) : {};
  } catch {
    payload = { message: text };
  }

  if (!response.ok || (typeof payload.code === 'number' && payload.code !== 0)) {
    const message = payload.message || payload.reason || response.statusText;
    throw new Error(`${method} ${pathname} failed: ${response.status} ${message}`);
  }

  return Object.prototype.hasOwnProperty.call(payload, 'data') ? payload.data : payload;
}

async function runLimited(items, limit, worker) {
  const results = new Array(items.length);
  let nextIndex = 0;

  async function loop() {
    while (nextIndex < items.length) {
      const index = nextIndex;
      nextIndex += 1;
      try {
        results[index] = { status: 'fulfilled', value: await worker(items[index]) };
      } catch (error) {
        results[index] = { status: 'rejected', reason: error };
      }
    }
  }

  const workers = Array.from({ length: Math.min(limit, items.length) }, () => loop());
  await Promise.all(workers);
  return results;
}

async function readState(file) {
  try {
    const raw = await fs.readFile(file, 'utf8');
    return JSON.parse(raw);
  } catch (error) {
    if (error.code === 'ENOENT') return {};
    throw error;
  }
}

async function writeState(file, data) {
  await fs.mkdir(path.dirname(file), { recursive: true });
  const tmp = `${file}.${process.pid}.tmp`;
  await fs.writeFile(tmp, `${JSON.stringify(data, null, 2)}\n`, 'utf8');
  await fs.rename(tmp, file);
}

function formatForDisplay(date) {
  const formatter = new Intl.DateTimeFormat('zh-CN', {
    timeZone: config.timeZone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
  return `${formatter.format(date)} (${config.timeZone})`;
}

function normalizeBaseURL(raw) {
  const trimmed = raw.replace(/\/+$/, '');
  return trimmed.endsWith('/api/v1') ? trimmed : `${trimmed}/api/v1`;
}

function normalizeNotifyMode(value) {
  const mode = String(value || '').trim();
  if (mode === 'silent' || mode === 'popup') return mode;
  return 'popup';
}

function addHours(date, hours) {
  return new Date(date.getTime() + hours * 60 * 60 * 1000);
}

function parseDate(value) {
  if (!value) return null;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? null : date;
}

function requiredEnv(name) {
  const value = process.env[name];
  if (!value || value.trim() === '') {
    throw new Error(`${name} is required`);
  }
  return value.trim();
}

function intEnv(name, fallback) {
  const value = process.env[name];
  if (value === undefined || value === '') return fallback;
  const parsed = Number.parseInt(value, 10);
  if (!Number.isInteger(parsed)) {
    throw new Error(`${name} must be an integer`);
  }
  return parsed;
}

function numberEnv(name, fallback) {
  const value = process.env[name];
  if (value === undefined || value === '') return fallback;
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    throw new Error(`${name} must be a number`);
  }
  return parsed;
}

function boolEnv(name, fallback) {
  const value = process.env[name];
  if (value === undefined || value === '') return fallback;
  return ['1', 'true', 'yes', 'on'].includes(value.toLowerCase());
}
