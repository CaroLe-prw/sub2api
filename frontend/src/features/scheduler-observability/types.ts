export type SchedulerTraceStatus = "pending" | "success" | "switched" | "failed" | "canceled";

export type SchedulerRequestType = "sync" | "stream" | "ws_v2";

export type SchedulerCandidateScope = "scored" | "sticky_short_circuit";

export type SchedulerDecisionLayer =
  | "previous_response_id"
  | "session_hash"
  | "load_balance";

export type SchedulerSessionSource =
  | "session_hash"
  | "session_header"
  | "prompt_cache_key"
  | "previous_response_id"
  | "content_fallback"
  | "none";

export type SchedulerDecisionSummary =
  | "sticky_escaped_consecutive_errors"
  | "sticky_escaped_concurrency"
  | "sticky_failed_over_upstream_error"
  | "session_sticky_kept"
  | "weighted_sticky_kept"
  | "previous_response_kept"
  | "load_balance_best_score"
  | "no_available_account";

export type SchedulerAttemptKind =
  | "sticky_detected"
  // Backward compatibility for traces persisted before the wording fix.
  | "sticky_match"
  | "sticky_selected"
  | "candidate_selected"
  | "upstream_failure"
  | "same_account_retry"
  | "sticky_escape"
  | "account_switch"
  | "account_reselected"
  | "admission_rejected"
  | "retry_continued"
  | "retry_stopped"
  | "request_canceled"
  | "request_success";

export interface SchedulerAttempt {
  id: string;
  kind: SchedulerAttemptKind;
  accountId?: number;
  accountName?: string;
  offsetMs: number;
  upstreamStatus?: number;
  retryCount?: number;
  retryLimit?: number;
  budgetMs?: number;
  remainingCandidates?: number;
  reason?: string;
}

export type SchedulerCandidateState = "selected" | "tried" | "eligible" | "excluded" | "rejected";

export interface SchedulerCandidate {
  accountId: number;
  accountName: string;
  rank: number;
  baseScore: number;
  stickyBonus: number;
  totalScore: number;
  state: SchedulerCandidateState;
  reason?: string;
}

export interface SchedulerTrace {
  id: string;
  requestId: string;
  createdAt: string;
  userId: number;
  userEmail: string;
  apiKeyId?: number;
  apiKeyName: string;
  groupId?: number;
  groupName: string;
  model: string;
  requestType: SchedulerRequestType;
  cyberBlocked: boolean;
  sessionFingerprint: string | null;
  sessionSource: SchedulerSessionSource;
  sessionTurn: number | null;
  decisionLayer: SchedulerDecisionLayer;
  candidateScope?: SchedulerCandidateScope;
  summary: SchedulerDecisionSummary;
  stickyDetected?: boolean;
  stickyHit: boolean;
  accountPath: Array<{ id: number; name: string }>;
  retryCount: number;
  switchCount: number;
  cacheReadTokens: number;
  cacheEligibleTokens: number;
  firstTokenMs: number | null;
  endToEndFirstTokenMs?: number | null;
  durationMs: number;
  status: SchedulerTraceStatus;
  attempts: SchedulerAttempt[];
  candidates: SchedulerCandidate[];
}

export interface SchedulerSessionSummary {
  fingerprint: string;
  source: SchedulerSessionSource;
  userId: number;
  userEmail: string;
  apiKeyName: string;
  groupId?: number;
  groupName: string;
  model: string;
  turns: number;
  accountIds: number[];
  accountNames: Record<string, string>;
  switchCount: number;
  stickyHitRate: number;
  followUpCacheRate: number;
  lastActiveAt: string;
  turnAccounts: number[];
}

export interface SchedulerOverviewMetric {
  key: "requests" | "sticky" | "switch" | "stability" | "cache";
  value: string;
  detail: string;
  trend: number | null;
  tone: "neutral" | "success" | "warning" | "danger";
}

export interface SchedulerObservabilityMetrics {
  requests: number;
  stickyDetectedRequests?: number;
  stickyRequests: number;
  stickyHitRate: number;
  switchedRequests: number;
  switches: number;
  switchRate: number;
  stableSessions: number;
  sessions: number;
  sessionStability: number;
  cacheReadTokens: number;
  cacheEligibleTokens: number;
  followUpCacheRate: number;
}

export interface SchedulerObservabilityReason {
  key: string;
  count: number;
}

export interface SchedulerObservabilityGroup {
  id: number;
  name: string;
}

export interface SchedulerObservabilityFilterOption {
  id: number;
  name: string;
}

export interface SchedulerObservabilitySnapshot {
  enabled: boolean;
  generatedAt: string;
  timeRange: "15m" | "1h" | "6h" | "24h" | "7d";
  view: "requests" | "sessions";
  retentionMode: "memory" | "hybrid";
  retentionMax: number;
  retentionDays: number;
  pagination: {
    page: number;
    pageSize: number;
    total: number;
    pages: number;
  };
  traceCounts: {
    all: number;
    sticky: number;
    switch: number;
    failed: number;
  };
  metrics: SchedulerObservabilityMetrics;
  switchReasons: SchedulerObservabilityReason[];
  groups: SchedulerObservabilityGroup[];
  models: string[];
  accounts: SchedulerObservabilityFilterOption[];
  apiKeys: SchedulerObservabilityFilterOption[];
  traces: SchedulerTrace[];
  sessions: SchedulerSessionSummary[];
}
