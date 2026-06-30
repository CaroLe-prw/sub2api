#!/usr/bin/env node

import { spawn } from 'node:child_process';

const DEFAULT_WINDOWS = ['daily', 'weekly'];
const ALLOWED_WINDOWS = new Set(['daily', 'weekly', 'monthly']);
const MANAGED_BLOCK_START = '<!-- sub2api-manual-quota-reset:start -->';
const MANAGED_BLOCK_END = '<!-- sub2api-manual-quota-reset:end -->';

let config;

main().catch((error) => {
  console.error(`[manual-quota-reset] failed: ${error.message}`);
  process.exitCode = 1;
});

async function main() {
  config = buildConfig();

  if (config.help) {
    printHelp();
    return;
  }
  if (config.pageSize <= 0) {
    throw new Error('PAGE_SIZE must be greater than 0');
  }
  if (config.concurrency <= 0) {
    throw new Error('RESET_CONCURRENCY must be greater than 0');
  }
  if (config.psqlRetryAttempts <= 0) {
    throw new Error('PSQL_RETRY_ATTEMPTS must be greater than 0');
  }
  if (config.psqlRetryDelayMs < 0) {
    throw new Error('PSQL_RETRY_DELAY_MS must be non-negative');
  }
  if (config.psqlRetryMaxDelayMs < config.psqlRetryDelayMs) {
    throw new Error('PSQL_RETRY_MAX_DELAY_MS must be greater than or equal to PSQL_RETRY_DELAY_MS');
  }
  if (config.windows.length === 0) {
    throw new Error('at least one reset window is required');
  }

  const announcement = await apiGet(`/admin/announcements/${config.announcementID}`);
  const groupIDs = extractGroupIDs(announcement.targeting);
  if (groupIDs.length === 0) {
    throw new Error(`announcement ${config.announcementID} has no targeting group_ids; refusing to reset all subscriptions`);
  }

  console.log(`[manual-quota-reset] announcement=${config.announcementID}`);
  console.log(`[manual-quota-reset] groups=${groupIDs.join(',')}`);
  console.log(`[manual-quota-reset] windows=${config.windows.join(',')}`);
  console.log(`[manual-quota-reset] window_start_mode=${config.manualWindowStartMode}`);
  console.log(`[manual-quota-reset] subscription_status=${config.subscriptionStatus}`);

  const subscriptions = await listSubscriptionsByGroups(groupIDs);
  const resettableSubscriptions = subscriptions.filter(hasAllQuotaWindowStarts);
  const skippedSubscriptions = subscriptions.filter((sub) => !hasAllQuotaWindowStarts(sub));
  console.log(
    `[manual-quota-reset] matched subscriptions=${subscriptions.length} resettable=${resettableSubscriptions.length} skipped_missing_windows=${skippedSubscriptions.length}`,
  );

  if (subscriptions.length === 0) {
    return;
  }

  for (const sub of resettableSubscriptions) {
    console.log(
      `[manual-quota-reset] target subscription=${sub.id} user=${sub.user?.email || sub.user_id || 'unknown'} group=${sub.group?.id || sub.group_id || 'unknown'}`,
    );
  }
  for (const sub of skippedSubscriptions) {
    console.log(
      `[manual-quota-reset] skip subscription=${sub.id} missing_windows=${missingQuotaWindowNames(sub).join(',')}`,
    );
  }

  if (!config.yes || config.dryRun) {
    console.log('[manual-quota-reset] dry-run only. Re-run with --yes to perform the reset.');
    return;
  }

  const resetBody = {
    daily: config.windows.includes('daily'),
    weekly: config.windows.includes('weekly'),
    monthly: config.windows.includes('monthly'),
  };
  if (resettableSubscriptions.length > 0) {
    await preflightManualWindowStartPatch(resetBody);
  }
  const results = await runLimited(resettableSubscriptions, config.concurrency, async (sub) => {
    await apiPost(`/admin/subscriptions/${sub.id}/reset-quota`, resetBody);
    return sub.id;
  });

  const failed = results.filter((item) => item.status === 'rejected');
  const succeeded = results.filter((item) => item.status === 'fulfilled');
  const resetFinishedAt = new Date();

  if (failed.length > 0) {
    for (const item of failed) {
      console.error(`[manual-quota-reset] reset failed: ${item.reason?.message || item.reason}`);
    }
    throw new Error(`reset finished with ${failed.length} failed subscription(s), succeeded=${succeeded.length}`);
  }

  if (succeeded.length > 0) {
    await applyManualWindowStartMode(
      succeeded.map((item) => item.value),
      resettableSubscriptions,
      resetBody,
      resetFinishedAt,
    );
  }

  if (config.updateAnnouncement && succeeded.length > 0) {
    await publishAnnouncementNotice(announcement, {
      resetAt: resetFinishedAt,
      groupIDs,
      resetCount: succeeded.length,
      failedCount: 0,
      windows: config.windows,
    });
  }

  console.log(`[manual-quota-reset] done: reset=${succeeded.length}`);
}

function buildConfig() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    return { help: true };
  }
  const windows = parseWindows(args.windows || process.env.RESET_WINDOWS || DEFAULT_WINDOWS.join(','));
  const manualWindowStartMode = parseManualWindowStartMode(args.windowStartMode || process.env.MANUAL_WINDOW_START_MODE || 'refresh');
  return {
    help: false,
    baseURL: normalizeBaseURL(args.baseURL || requiredEnv('SUB2API_BASE_URL')),
    adminAPIKey: args.adminAPIKey || requiredEnv('SUB2API_ADMIN_API_KEY'),
    announcementID: toInteger(args.announcementID || process.env.ANNOUNCEMENT_ID || '2', 'ANNOUNCEMENT_ID'),
    pageSize: toInteger(args.pageSize || process.env.PAGE_SIZE || '500', 'PAGE_SIZE'),
    concurrency: toInteger(args.concurrency || process.env.RESET_CONCURRENCY || '5', 'RESET_CONCURRENCY'),
    subscriptionStatus: args.status || process.env.SUBSCRIPTION_STATUS || 'active',
    windows,
    dryRun: args.dryRun || boolEnv('DRY_RUN', false),
    yes: args.yes || boolEnv('YES', false),
    updateAnnouncement: boolEnv('UPDATE_ANNOUNCEMENT', true),
    announcementPublishMode: process.env.ANNOUNCEMENT_PUBLISH_MODE || 'create',
    createdAnnouncementNotifyMode: process.env.CREATED_ANNOUNCEMENT_NOTIFY_MODE || '',
    manualAnnouncementTitle: process.env.MANUAL_CREATED_ANNOUNCEMENT_TITLE || '',
    manualAnnouncementTTLHours: numberEnv('MANUAL_ANNOUNCEMENT_TTL_HOURS', 24),
    manualQuotaResetLabel: process.env.MANUAL_QUOTA_RESET_LABEL || '',
    manualWindowStartMode,
    databaseURL: process.env.DATABASE_URL || '',
    databaseHost: process.env.DATABASE_HOST || process.env.POSTGRES_HOST || 'postgres',
    databasePort: process.env.DATABASE_PORT || process.env.POSTGRES_PORT || '5432',
    databaseUser: process.env.DATABASE_USER || process.env.POSTGRES_USER || 'sub2api',
    databasePassword: process.env.DATABASE_PASSWORD || process.env.POSTGRES_PASSWORD || '',
    databaseName: process.env.DATABASE_DBNAME || process.env.POSTGRES_DB || 'sub2api',
    databaseSSLMode: process.env.DATABASE_SSLMODE || 'disable',
    psqlRetryAttempts: toInteger(process.env.PSQL_RETRY_ATTEMPTS || '5', 'PSQL_RETRY_ATTEMPTS'),
    psqlRetryDelayMs: toInteger(process.env.PSQL_RETRY_DELAY_MS || '3000', 'PSQL_RETRY_DELAY_MS'),
    psqlRetryMaxDelayMs: toInteger(process.env.PSQL_RETRY_MAX_DELAY_MS || '30000', 'PSQL_RETRY_MAX_DELAY_MS'),
  };
}

function parseArgs(argv) {
  const out = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === '-h' || arg === '--help') {
      out.help = true;
      continue;
    }
    if (arg === '--yes' || arg === '-y') {
      out.yes = true;
      continue;
    }
    if (arg === '--dry-run') {
      out.dryRun = true;
      continue;
    }

    const [key, inlineValue] = arg.split('=', 2);
    const readValue = () => {
      if (inlineValue !== undefined) return inlineValue;
      i += 1;
      if (i >= argv.length) throw new Error(`${key} requires a value`);
      return argv[i];
    };

    switch (key) {
      case '--base-url':
        out.baseURL = readValue();
        break;
      case '--admin-api-key':
        out.adminAPIKey = readValue();
        break;
      case '--announcement-id':
        out.announcementID = readValue();
        break;
      case '--windows':
        out.windows = readValue();
        break;
      case '--status':
        out.status = readValue();
        break;
      case '--page-size':
        out.pageSize = readValue();
        break;
      case '--concurrency':
        out.concurrency = readValue();
        break;
      case '--window-start-mode':
        out.windowStartMode = readValue();
        break;
      default:
        throw new Error(`unknown argument: ${arg}`);
    }
  }
  return out;
}

async function listSubscriptionsByGroups(groupIDs) {
  const byID = new Map();
  for (const groupID of groupIDs) {
    let page = 1;
    while (true) {
      const query = new URLSearchParams({
        group_id: String(groupID),
        page: String(page),
        page_size: String(config.pageSize),
      });
      if (config.subscriptionStatus && config.subscriptionStatus !== 'all') {
        query.set('status', config.subscriptionStatus);
      }

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

async function publishAnnouncementNotice(announcement, summary) {
  if (config.announcementPublishMode === 'update') {
    await updateAnnouncementBlock(announcement, summary);
    return;
  }
  if (config.announcementPublishMode !== 'create') {
    throw new Error('ANNOUNCEMENT_PUBLISH_MODE must be update or create');
  }
  await createAnnouncementNotice(announcement, summary);
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
  console.log(`[manual-quota-reset] announcement ${config.announcementID} updated`);
}

async function createAnnouncementNotice(sourceAnnouncement, summary) {
  const resetAt = summary.resetAt || new Date();
  const payload = {
    title: buildCreatedAnnouncementTitle(summary),
    content: buildManagedBlock(summary),
    status: 'active',
    notify_mode: normalizeNotifyMode(config.createdAnnouncementNotifyMode || sourceAnnouncement.notify_mode || 'popup'),
    targeting: sourceAnnouncement.targeting,
    starts_at: Math.floor(resetAt.getTime() / 1000),
  };

  if (config.manualAnnouncementTTLHours > 0) {
    payload.ends_at = Math.floor(addHours(resetAt, config.manualAnnouncementTTLHours).getTime() / 1000);
  }

  const created = await apiPost('/admin/announcements', payload);
  console.log(`[manual-quota-reset] announcement notice created id=${created.id || 'unknown'}`);
}

function buildCreatedAnnouncementTitle(summary) {
  if (config.manualAnnouncementTitle.trim() !== '') {
    return config.manualAnnouncementTitle.trim();
  }
  return `${buildQuotaResetLabel(summary)}已手动重置`;
}

function buildManagedBlock(summary) {
  const resetLabel = buildQuotaResetLabel(summary);
  const lines = [
    MANAGED_BLOCK_START,
    `### ${resetLabel}手动重置通知`,
    '',
    '- 当前状态：已重置',
    `- 本次重置范围：${resetLabel}`,
    `- 重置时间：${summary.resetAt ? formatForDisplay(summary.resetAt) : formatForDisplay(new Date())}`,
    '- 处理结果：重置成功',
    '',
    MANAGED_BLOCK_END,
  ];
  return lines.join('\n');
}

function buildQuotaResetLabel(summary) {
  const configured = config.manualQuotaResetLabel.trim();
  if (configured !== '') {
    return configured;
  }
  return summary.windows.map(formatWindowLabel).join('、');
}

function formatWindowLabel(window) {
  switch (window) {
    case 'daily':
      return '每日配额';
    case 'weekly':
      return '每周配额';
    case 'monthly':
      return '每月配额';
    default:
      return window;
  }
}

function replaceManagedBlock(content, block) {
  const start = content.indexOf(MANAGED_BLOCK_START);
  const end = content.indexOf(MANAGED_BLOCK_END);
  if (start !== -1 && end !== -1 && end > start) {
    return `${content.slice(0, start).trimEnd()}\n\n${block}\n\n${content.slice(end + MANAGED_BLOCK_END.length).trimStart()}`.trim();
  }
  return `${content.trimEnd()}\n\n${block}`.trim();
}

async function applyManualWindowStartMode(subscriptionIDs, subscriptions, resetBody, resetAt) {
  const windows = selectedWindows(resetBody);
  if (windows.length === 0) {
    return;
  }

  if (config.manualWindowStartMode === 'refresh') {
    const refreshWindows = windows.filter((window) => window !== 'monthly');
    const preserveWindows = windows.filter((window) => window === 'monthly');
    if (refreshWindows.length > 0) {
      await patchQuotaWindowStarts(subscriptionIDs, refreshWindows, resetAt);
    }
    if (preserveWindows.length > 0) {
      await preserveQuotaWindowStarts(subscriptionIDs, subscriptions, preserveWindows);
    }
    return;
  }

  if (config.manualWindowStartMode === 'preserve') {
    await preserveQuotaWindowStarts(subscriptionIDs, subscriptions, windows);
    return;
  }

  throw new Error(`unsupported MANUAL_WINDOW_START_MODE: ${config.manualWindowStartMode}`);
}

async function preflightManualWindowStartPatch(resetBody) {
  const windows = selectedWindows(resetBody);
  if (windows.length === 0) {
    return;
  }
  await runPsql('SELECT 1;');
  console.log(`[manual-quota-reset] quota window start patch preflight ok windows=${windows.join(',')}`);
}

function selectedWindows(resetBody) {
  return ['daily', 'weekly', 'monthly'].filter((window) => resetBody[window]);
}

async function patchQuotaWindowStarts(subscriptionIDs, windows, windowStart) {
  const ids = subscriptionIDs
    .map((id) => Number(id))
    .filter((id) => Number.isInteger(id) && id > 0);
  if (ids.length === 0) {
    return;
  }

  const windowStartSQL = sqlTimestamp(windowStart);
  const setParts = [];
  for (const window of windows) {
    const columns = quotaWindowColumns(window);
    setParts.push(`${columns.usage} = 0`);
    setParts.push(`${columns.start} = ${windowStartSQL}`);
  }
  setParts.push('updated_at = NOW()');
  const sql = [
    'UPDATE user_subscriptions',
    `SET ${setParts.join(', ')}`,
    `WHERE id IN (${ids.join(',')});`,
  ].join(' ');

  await runPsql(sql);
  console.log(`[manual-quota-reset] quota window starts refreshed=${ids.length} windows=${windows.join(',')} value=${windowStart.toISOString()}`);
}

async function preserveQuotaWindowStarts(subscriptionIDs, subscriptions, windows) {
  const ids = subscriptionIDs
    .map((id) => Number(id))
    .filter((id) => Number.isInteger(id) && id > 0);
  if (ids.length === 0) {
    return;
  }

  const byID = new Map(subscriptions.map((sub) => [Number(sub.id), sub]));
  const setParts = [];
  for (const window of windows) {
    const columns = quotaWindowColumns(window);
    setParts.push(`${columns.usage} = 0`);
    setParts.push(`${columns.start} = CASE id ${ids.map((id) => {
      const sub = byID.get(id);
      if (!sub) {
        throw new Error(`subscription ${id} is missing from original reset list`);
      }
      return `WHEN ${id} THEN ${sqlTimestamp(sub[columns.payloadStart])}`;
    }).join(' ')} ELSE ${columns.start} END`);
  }
  setParts.push('updated_at = NOW()');

  const sql = [
    'UPDATE user_subscriptions',
    `SET ${setParts.join(', ')}`,
    `WHERE id IN (${ids.join(',')});`,
  ].join(' ');

  await runPsql(sql);
  console.log(`[manual-quota-reset] quota window starts preserved=${ids.length} windows=${windows.join(',')}`);
}

function quotaWindowColumns(window) {
  switch (window) {
    case 'daily':
      return { usage: 'daily_usage_usd', start: 'daily_window_start', payloadStart: 'daily_window_start' };
    case 'weekly':
      return { usage: 'weekly_usage_usd', start: 'weekly_window_start', payloadStart: 'weekly_window_start' };
    case 'monthly':
      return { usage: 'monthly_usage_usd', start: 'monthly_window_start', payloadStart: 'monthly_window_start' };
    default:
      throw new Error(`invalid window ${window}`);
  }
}

function sqlTimestamp(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    throw new Error(`invalid quota window timestamp: ${value}`);
  }
  return `TIMESTAMPTZ '${date.toISOString()}'`;
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

  await runPsqlWithRetry(args, env);
}

async function runPsqlWithRetry(args, env) {
  for (let attempt = 1; attempt <= config.psqlRetryAttempts; attempt += 1) {
    try {
      await runCommand('psql', args, env);
      if (attempt > 1) {
        console.log(`[manual-quota-reset] psql succeeded after retry attempt=${attempt}`);
      }
      return;
    } catch (error) {
      if (attempt >= config.psqlRetryAttempts || !isRetryablePsqlError(error)) {
        throw error;
      }

      const delayMs = psqlRetryDelayMs(attempt);
      console.warn(
        `[manual-quota-reset] psql transient failure attempt=${attempt}/${config.psqlRetryAttempts}; retrying in ${delayMs}ms: ${error.message}`,
      );
      await sleep(delayMs);
    }
  }
}

function isRetryablePsqlError(error) {
  const message = String(error?.message || error).toLowerCase();
  return [
    'too many clients already',
    'remaining connection slots are reserved',
    'connection refused',
    'could not connect to server',
    'server closed the connection unexpectedly',
    'the database system is starting up',
    'the database system is shutting down',
    'timeout expired',
    'connection reset by peer',
  ].some((pattern) => message.includes(pattern));
}

function psqlRetryDelayMs(failedAttempt) {
  const delay = config.psqlRetryDelayMs * (2 ** (failedAttempt - 1));
  return Math.min(delay, config.psqlRetryMaxDelayMs);
}

function sleep(ms) {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
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

function parseWindows(raw) {
  const windows = String(raw)
    .split(',')
    .map((item) => item.trim().toLowerCase())
    .filter(Boolean);
  const unique = [...new Set(windows)];
  for (const window of unique) {
    if (!ALLOWED_WINDOWS.has(window)) {
      throw new Error(`invalid window ${window}; allowed values: daily, weekly, monthly`);
    }
  }
  return unique;
}

function parseManualWindowStartMode(raw) {
  const mode = String(raw || 'refresh').trim().toLowerCase();
  if (['refresh', 'renew', 'refresh-validity'].includes(mode)) {
    return 'refresh';
  }
  if (['preserve', 'keep', 'no-refresh', 'no-refresh-validity'].includes(mode)) {
    return 'preserve';
  }
  throw new Error('MANUAL_WINDOW_START_MODE must be refresh or preserve');
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

function formatForDisplay(date) {
  const formatter = new Intl.DateTimeFormat('zh-CN', {
    timeZone: process.env.TZ || process.env.DISPLAY_TIME_ZONE || 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  });
  return `${formatter.format(date)} (${process.env.TZ || process.env.DISPLAY_TIME_ZONE || 'Asia/Shanghai'})`;
}

function addHours(date, hours) {
  return new Date(date.getTime() + hours * 60 * 60 * 1000);
}

function requiredEnv(name) {
  const value = process.env[name];
  if (!value || value.trim() === '') {
    throw new Error(`${name} is required`);
  }
  return value.trim();
}

function toInteger(raw, name) {
  const parsed = Number.parseInt(String(raw), 10);
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

function printHelp() {
  console.log(`
用法:
  node tools/manual-reset-subscription-quota-from-announcement.mjs [options]

必要环境变量:
  SUB2API_BASE_URL
  SUB2API_ADMIN_API_KEY

参数:
  --yes, -y                 执行重置。不加时只预览命中的订阅。
  --dry-run                 强制预览模式。
  --announcement-id <id>    分组来源公告 ID。默认：ANNOUNCEMENT_ID 或 2。
  --windows <list>          要重置的窗口，逗号分隔。默认：daily,weekly。
  --window-start-mode <mode> 窗口有效期模式：refresh 刷新日/周有效期，preserve 不刷新日/周有效期。月限额始终保留窗口时间。默认：refresh。
  --status <status>         订阅状态筛选。默认：active。传 all 表示不按状态筛选。
  --page-size <n>           分页大小。默认：500。
  --concurrency <n>         重置并发数。默认：5。
  --base-url <url>          覆盖 SUB2API_BASE_URL。
  --admin-api-key <key>     覆盖 SUB2API_ADMIN_API_KEY。

示例:
  node tools/manual-reset-subscription-quota-from-announcement.mjs
  node tools/manual-reset-subscription-quota-from-announcement.mjs --yes
  node tools/manual-reset-subscription-quota-from-announcement.mjs --yes --window-start-mode preserve
  node tools/manual-reset-subscription-quota-from-announcement.mjs --yes --windows daily,weekly,monthly
`.trim());
}
