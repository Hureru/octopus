import {
  type Site,
  type SiteAccount,
  SitePlatform,
} from "@/api/endpoints/site";

export type CheckinFilterStatus =
  | "all"
  | "success"
  | "failed"
  | "idle"
  | "disabled";

export type DerivedCheckinStatus = Exclude<CheckinFilterStatus, "all">;

export type CheckinSummary = {
  total: number;
  success: number;
  failed: number;
  idle: number;
  disabled: number;
};

function normalizeExecutionStatus(status?: string | null) {
  return status || "idle";
}

export function createEmptyCheckinSummary(): CheckinSummary {
  return {
    total: 0,
    success: 0,
    failed: 0,
    idle: 0,
    disabled: 0,
  };
}

export function sitePlatformSupportsCheckin(platform: Site["platform"]) {
  switch (platform) {
    case SitePlatform.DoneHub:
    case SitePlatform.Sub2API:
    case SitePlatform.OpenAI:
    case SitePlatform.Claude:
    case SitePlatform.Gemini:
      return false;
    default:
      return true;
  }
}

export function accountHasCheckinEnabled(
  account: Pick<SiteAccount, "auto_checkin">,
  platform: Site["platform"],
) {
  return sitePlatformSupportsCheckin(platform) && account.auto_checkin;
}

export function accountIsDisabled(
  site: Pick<Site, "enabled">,
  account: Pick<SiteAccount, "enabled">,
) {
  return !site.enabled || !account.enabled;
}

function happenedToday(value?: string | null, now = new Date()) {
  if (!value) return false;
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) {
    return false;
  }

  return (
    date.getFullYear() === now.getFullYear() &&
    date.getMonth() === now.getMonth() &&
    date.getDate() === now.getDate()
  );
}

export function deriveCheckinStatus(
  site: Pick<Site, "enabled" | "platform">,
  account: Pick<
    SiteAccount,
    "enabled" | "auto_checkin" | "last_checkin_at" | "last_checkin_status"
  >,
  now = new Date(),
): DerivedCheckinStatus | null {
  if (accountIsDisabled(site, account)) {
    return "disabled";
  }

  if (!accountHasCheckinEnabled(account, site.platform)) {
    return null;
  }

  if (!happenedToday(account.last_checkin_at, now)) {
    return "idle";
  }

  switch (normalizeExecutionStatus(account.last_checkin_status)) {
    case "success":
      return "success";
    case "failed":
    case "skipped":
      return "failed";
    default:
      return "idle";
  }
}

export function accountMatchesCheckinFilter(
  site: Pick<Site, "enabled" | "platform">,
  account: Pick<
    SiteAccount,
    "enabled" | "auto_checkin" | "last_checkin_at" | "last_checkin_status"
  >,
  filterStatus: CheckinFilterStatus,
  now = new Date(),
) {
  if (filterStatus === "all") {
    return true;
  }

  return deriveCheckinStatus(site, account, now) === filterStatus;
}

export function buildCheckinSummary(
  sites: Site[] | undefined,
  now = new Date(),
): CheckinSummary {
  const summary = createEmptyCheckinSummary();

  for (const site of sites ?? []) {
    for (const account of site.accounts ?? []) {
      const status = deriveCheckinStatus(site, account, now);
      if (!status) {
        continue;
      }

      summary.total += 1;
      summary[status] += 1;
    }
  }

  return summary;
}
