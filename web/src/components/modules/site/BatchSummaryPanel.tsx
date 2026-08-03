"use client";

import { useMemo } from "react";
import { useTranslations } from "next-intl";
import {
  AlertOctagon,
  AlertTriangle,
  RefreshCcw,
  ShieldAlert,
  CircleDashed,
  FilterX,
} from "lucide-react";
import {
  useSiteBatchSummary,
  type SiteBatchFailureGroup,
} from "@/api/endpoints/site";
import { cn } from "@/lib/utils";

export type FailureSeverity = "credential" | "risk" | "transient" | "other";

const FAILURE_META: Record<
  string,
  { severity: FailureSeverity; hide?: boolean }
> = {
  unauthorized: { severity: "credential" },
  login_failed: { severity: "credential" },
  access_token_required: { severity: "credential" },
  direct_token_required: { severity: "credential" },
  cloudflare_protection: { severity: "risk" },
  missing_group_key: { severity: "transient" },
  upstream_http_error: { severity: "transient" },
  upstream_decode_failed: { severity: "transient" },
  upstream_html_response: { severity: "transient" },
  timeout: { severity: "transient" },
  unsupported_platform: { severity: "transient" },
  unsupported_checkin: { severity: "transient" },
  scheduled_later: { severity: "other", hide: true },
  batch_canceled: { severity: "other", hide: true },
  context_canceled: { severity: "other", hide: true },
  context_deadline_exceeded: { severity: "other" },
  database_error: { severity: "other" },
  internal_error: { severity: "other" },
  unknown: { severity: "other" },
};

function failureMeta(reason: string) {
  return FAILURE_META[reason] ?? { severity: "other" as FailureSeverity };
}

function severityTone(severity: FailureSeverity) {
  switch (severity) {
    case "credential":
      return {
        dot: "bg-red-500",
        chip: "border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300",
      };
    case "risk":
      return {
        dot: "bg-orange-500",
        chip: "border-orange-500/30 bg-orange-500/10 text-orange-700 dark:text-orange-300",
      };
    case "transient":
      return {
        dot: "bg-yellow-400",
        chip: "border-yellow-500/30 bg-yellow-500/10 text-yellow-700 dark:text-yellow-300",
      };
    default:
      return {
        dot: "bg-slate-400",
        chip: "border-slate-500/30 bg-slate-500/10 text-slate-600 dark:text-slate-300",
      };
  }
}

function severityIcon(severity: FailureSeverity) {
  switch (severity) {
    case "credential":
      return <AlertOctagon className="size-3.5" />;
    case "risk":
      return <ShieldAlert className="size-3.5" />;
    case "transient":
      return <AlertTriangle className="size-3.5" />;
    default:
      return <CircleDashed className="size-3.5" />;
  }
}

function formatDuration(ms: number) {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export function BatchSummaryPanel() {
  const t = useTranslations("siteManagement.checkin.batchSummary");
  const sync = useSiteBatchSummary("sync");
  const checkin = useSiteBatchSummary("checkin");

  const groups = useMemo(() => {
    const items: Array<{
      phase: "sync" | "checkin";
      group: SiteBatchFailureGroup;
    }> = [];
    for (const data of [sync.data, checkin.data]) {
      if (!data || data.failed === 0) continue;
      for (const group of data.failure_groups) {
        const meta = failureMeta(group.reason);
        if (meta.hide || group.failed === 0) continue;
        items.push({
          phase: data.phase,
          group: {
            ...group,
            count: group.failed,
          },
        });
      }
    }
    const order = ["credential", "risk", "transient", "other"] as const;
    return items.sort((a, b) => {
      const sa = order.indexOf(failureMeta(a.group.reason).severity);
      const sb = order.indexOf(failureMeta(b.group.reason).severity);
      if (sa !== sb) return sa - sb;
      return b.group.count - a.group.count;
    });
  }, [sync.data, checkin.data]);

  const totalDuration = (sync.data?.duration_ms ?? 0) + (checkin.data?.duration_ms ?? 0);
  const finishedAt = useMemo(() => {
    const items = [sync.data, checkin.data].filter(
      (d): d is NonNullable<typeof d> => Boolean(d?.finished_at),
    );
    if (items.length === 0) return "";
    return items.reduce((a, b) =>
      a.finished_at > b.finished_at ? a : b,
    ).finished_at;
  }, [sync.data, checkin.data]);

  if (!sync.data && !checkin.data) {
    return null;
  }
  const hasGroups = groups.length > 0;
  const totalFailed = (sync.data?.failed ?? 0) + (checkin.data?.failed ?? 0);

  return (
    <section
      className={cn(
        "overflow-hidden rounded-[28px] border bg-card shadow-[0_18px_60px_-40px_rgba(15,23,42,0.45)]",
        hasGroups ? "border-red-500/25" : "border-border/70",
      )}
    >
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-border/60 px-5 py-3.5">
        <div className="flex items-center gap-2 text-sm font-semibold">
          <RefreshCcw
            className={cn(
              "size-4",
              hasGroups ? "text-red-500" : "text-primary",
            )}
          />
          <span>{t("title")}</span>
          {totalFailed > 0 && (
            <span className="rounded-full bg-destructive/10 px-2 py-px text-[10px] font-medium text-destructive">
              {t("failedCount", { count: totalFailed })}
            </span>
          )}
        </div>
        {finishedAt ? (
          <span className="text-[11px] text-muted-foreground">
            {t("finishedAt")} {new Date(finishedAt).toLocaleString()}
            {totalDuration > 0 ? ` · ${formatDuration(totalDuration)}` : ""}
          </span>
        ) : null}
      </div>

      <div className="px-5 py-4">
        {!hasGroups ? (
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <FilterX className="size-4" />
            {t("noFailures")}
          </div>
        ) : (
          <div className="space-y-2">
            {groups.map(({ phase, group }) => {
              const meta = failureMeta(group.reason);
              const tone = severityTone(meta.severity);
              const sample =
                phase === "sync"
                  ? sync.data?.samples?.find(
                      (s) =>
                        s.site_id === group.site_id && s.reason === group.reason,
                    )
                  : checkin.data?.samples?.find(
                      (s) =>
                        s.site_id === group.site_id && s.reason === group.reason,
                    );
              return (
                <div
                  key={`${phase}-${group.site_id}-${group.reason}`}
                  className={cn(
                    "flex flex-wrap items-center gap-2 rounded-xl border px-3 py-2",
                    tone.chip,
                  )}
                >
                  {severityIcon(meta.severity)}
                  <span className="rounded-full border border-current/30 px-1.5 py-px text-[10px] font-medium">
                    {phase === "sync" ? t("phaseSync") : t("phaseCheckin")}
                  </span>
                  <span className="text-xs font-semibold">
                    {t(`reasons.${group.reason}`, {
                      defaultValue: group.reason,
                    })}
                  </span>
                  <span className="text-[11px] opacity-80">
                    {t("siteId", { id: group.site_id })}
                  </span>
                  <span className="ml-auto text-xs font-bold">
                    ×{group.count}
                  </span>
                  {sample?.message ? (
                    <div className="w-full truncate text-[11px] opacity-75">
                      {sample.message}
                    </div>
                  ) : null}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </section>
  );
}
