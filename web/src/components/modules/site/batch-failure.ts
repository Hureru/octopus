import type { SiteBatchSummary } from "@/api/endpoints/site";

export type BatchFailureCategory =
  | "credential"
  | "risk"
  | "transient"
  | "other";

export type BatchFailureSiteIds = Record<
  BatchFailureCategory,
  Set<number>
>;

const FAILURE_CATEGORIES: Record<string, BatchFailureCategory | null> = {
  unauthorized: "credential",
  login_failed: "credential",
  access_token_required: "credential",
  direct_token_required: "credential",
  cloudflare_protection: "risk",
  missing_group_key: "credential",
  upstream_http_error: "transient",
  upstream_decode_failed: "transient",
  upstream_html_response: "transient",
  timeout: "transient",
  unsupported_platform: "other",
  unsupported_checkin: "other",
  scheduled_later: null,
  batch_canceled: null,
  context_canceled: null,
  context_deadline_exceeded: "other",
  database_error: "other",
  internal_error: "other",
  unknown: "other",
};

function createEmptyFailureSiteIds(): BatchFailureSiteIds {
  return {
    credential: new Set(),
    risk: new Set(),
    transient: new Set(),
    other: new Set(),
  };
}

export function getBatchFailureCategory(
  reason: string,
): BatchFailureCategory | null {
  const category = FAILURE_CATEGORIES[reason];
  return category === undefined ? "other" : category;
}

export function buildBatchFailureSiteIds(
  summaries: Array<SiteBatchSummary | null | undefined>,
): BatchFailureSiteIds {
  const result = createEmptyFailureSiteIds();

  for (const summary of summaries) {
    for (const group of summary?.failure_groups ?? []) {
      if (group.failed <= 0) continue;
      const category = getBatchFailureCategory(group.reason);
      if (category) result[category].add(group.site_id);
    }
  }

  return result;
}
