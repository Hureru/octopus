"use client";

import { useCallback, useMemo, type ReactNode } from "react";
import { useTranslations } from "next-intl";
import {
  AlertTriangle,
  CalendarCheck2,
  ExternalLink,
  FilterX,
  Layers3,
  Tag,
  TrendingUp,
  Wallet,
} from "lucide-react";
import { type Site } from "@/api/endpoints/site";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  buildCheckinSummary,
  type CheckinActiveFilterStatus,
  type CheckinFilterStatus,
} from "./checkin-status";
import type { BatchFailureCategory } from "./batch-failure";

const FILTERS: Array<{
  key: CheckinFilterStatus;
  labelKey:
    | "filters.all"
    | "filters.success"
    | "filters.partial"
    | "filters.failed"
    | "filters.idle"
    | "filters.disabled"
    | "filters.reserve";
}> = [
  { key: "all", labelKey: "filters.all" },
  { key: "success", labelKey: "filters.success" },
  { key: "partial", labelKey: "filters.partial" },
  { key: "failed", labelKey: "filters.failed" },
  { key: "idle", labelKey: "filters.idle" },
  { key: "disabled", labelKey: "filters.disabled" },
  { key: "reserve", labelKey: "filters.reserve" },
];

const FAILURE_FILTERS: Array<{
  key: BatchFailureCategory;
  labelKey:
    | "filters.failureCredential"
    | "filters.failureRisk"
    | "filters.failureTransient"
    | "filters.failureOther";
}> = [
  { key: "credential", labelKey: "filters.failureCredential" },
  { key: "risk", labelKey: "filters.failureRisk" },
  { key: "transient", labelKey: "filters.failureTransient" },
  { key: "other", labelKey: "filters.failureOther" },
];

function filterTone(status: CheckinFilterStatus, active: boolean) {
  if (active) {
    switch (status) {
      case "success":
        return "border-emerald-500/30 bg-emerald-500 text-white";
      case "partial":
        return "border-amber-500/30 bg-amber-500 text-white";
      case "failed":
        return "border-destructive/30 bg-destructive text-white";
      case "idle":
        return "border-border bg-foreground text-background";
      case "disabled":
        return "border-slate-500/30 bg-slate-700 text-white dark:bg-slate-200 dark:text-slate-900";
      case "reserve":
        return "border-amber-500/30 bg-amber-500 text-white";
      case "all":
      default:
        return "border-primary/30 bg-primary text-primary-foreground";
    }
  }

  switch (status) {
    case "success":
      return "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "partial":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "failed":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "idle":
      return "border-border bg-muted/40 text-muted-foreground";
    case "disabled":
      return "border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-300";
    case "reserve":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "all":
    default:
      return "border-border bg-background text-foreground";
  }
}

function failureFilterTone(category: BatchFailureCategory, active: boolean) {
  if (active) {
    switch (category) {
      case "credential":
        return "border-red-500/30 bg-red-500 text-white";
      case "risk":
        return "border-orange-500/30 bg-orange-500 text-white";
      case "transient":
        return "border-amber-500/30 bg-amber-500 text-white";
      case "other":
        return "border-slate-500/30 bg-slate-700 text-white dark:bg-slate-200 dark:text-slate-900";
    }
  }

  switch (category) {
    case "credential":
      return "border-red-500/20 bg-red-500/10 text-red-700 dark:text-red-300";
    case "risk":
      return "border-orange-500/20 bg-orange-500/10 text-orange-700 dark:text-orange-300";
    case "transient":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "other":
      return "border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-300";
  }
}

function formatCurrency(value: number) {
  const safe = Number.isFinite(value) ? value : 0;
  return `$${safe.toFixed(2)}`;
}

function OverviewMetric({
  icon,
  label,
  value,
  tone,
}: {
  icon: ReactNode;
  label: string;
  value: string;
  tone?: "default" | "warning";
}) {
  return (
    <div className="flex items-center gap-3 rounded-2xl bg-muted/20 px-4 py-3">
      <span
        className={cn(
          "flex size-9 items-center justify-center rounded-xl bg-background shadow-sm",
          tone === "warning"
            ? "text-amber-600 dark:text-amber-400"
            : "text-muted-foreground",
        )}
      >
        {icon}
      </span>
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-base font-semibold truncate">{value}</div>
      </div>
    </div>
  );
}

export function CheckinPanel({
  sites,
  inventory,
  statusDayKey,
  visibleSiteCount,
  visibleAccountCount,
  searchTerm,
  hasActiveFilters,
  onClearFilters,
  activeFilterStatuses,
  onFilterChange,
  failureFilterCounts,
  activeFailureFilters,
  onFailureFilterChange,
  allTags,
  activeTags,
  onTagFilterChange,
}: {
  sites: Site[] | undefined;
  inventory: {
    totalBalance: number;
    totalBalanceUsed: number;
    enabledAccounts: number;
    totalAccounts: number;
  };
  statusDayKey: string;
  visibleSiteCount: number;
  visibleAccountCount: number;
  searchTerm: string;
  hasActiveFilters: boolean;
  onClearFilters: () => void;
  activeFilterStatuses: CheckinActiveFilterStatus[];
  onFilterChange: (status: CheckinFilterStatus) => void;
  failureFilterCounts: Record<BatchFailureCategory, number>;
  activeFailureFilters: BatchFailureCategory[];
  onFailureFilterChange: (category: BatchFailureCategory) => void;
  allTags: Array<{ tag: string; count: number }>;
  activeTags: string[];
  onTagFilterChange: (tag: string) => void;
}) {
  const t = useTranslations("siteManagement.checkin");
  const summaryNow = useMemo(() => {
    const [year = "", month = "", day = ""] = statusDayKey.split("-");
    const parsed = new Date(Number(year), Number(month), Number(day));
    return Number.isNaN(parsed.getTime()) ? new Date() : parsed;
  }, [statusDayKey]);

  const summary = useMemo(
    () => buildCheckinSummary(sites, summaryNow),
    [sites, summaryNow],
  );
  const hasContextBadges = Boolean(searchTerm);

  const manualCheckinUrls = useMemo(
    () =>
      (sites ?? [])
        .filter((s) => s.external_checkin_url?.trim())
        .map((s) => s.external_checkin_url!.trim()),
    [sites],
  );

  const openAllManualCheckin = useCallback(() => {
    for (const url of manualCheckinUrls) {
      window.open(url, "_blank", "noopener,noreferrer");
    }
  }, [manualCheckinUrls]);

  return (
    <section className="overflow-hidden rounded-[28px] border border-border/70 bg-card shadow-[0_18px_60px_-40px_rgba(15,23,42,0.45)]">
      <div className="border-b border-border/60 bg-gradient-to-br from-background via-card to-muted/10 px-5 py-5">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex flex-wrap items-center gap-2 text-base font-semibold">
            <CalendarCheck2 className="size-5 text-primary" />
            <span>{t("overview")}</span>
          </div>

          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>{t("currentResult")}</span>
            <span className="font-medium text-foreground">
              {t("visibleCounts", {
                siteCount: visibleSiteCount,
                accountCount: visibleAccountCount,
              })}
            </span>
          </div>
        </div>

        <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <OverviewMetric
            icon={<Wallet className="size-4" />}
            label={t("currentBalance")}
            value={formatCurrency(inventory.totalBalance)}
          />
          <OverviewMetric
            icon={<TrendingUp className="size-4" />}
            label={t("totalUsed")}
            value={formatCurrency(inventory.totalBalanceUsed)}
          />
          <OverviewMetric
            icon={<Layers3 className="size-4" />}
            label={t("enabledAccounts")}
            value={`${inventory.enabledAccounts} / ${inventory.totalAccounts}`}
          />
          <OverviewMetric
            icon={<AlertTriangle className="size-4" />}
            label={t("todayFailures")}
            value={`${summary.failed}`}
            tone={summary.failed > 0 ? "warning" : "default"}
          />
        </div>

        {hasActiveFilters && hasContextBadges ? (
          <div className="mt-4 flex flex-wrap gap-2">
            {searchTerm ? (
              <Badge variant="outline">{t("search", { term: searchTerm })}</Badge>
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="px-5 py-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex flex-wrap gap-2">
            {FILTERS.map((filter) => {
              const count =
                filter.key === "all" ? summary.total : summary[filter.key];
              const active =
                filter.key === "all"
                  ? activeFilterStatuses.length === 0 &&
                    activeFailureFilters.length === 0
                  : activeFilterStatuses.includes(filter.key);
              return (
                <button
                  key={filter.key}
                  type="button"
                  onClick={() => onFilterChange(filter.key)}
                  aria-pressed={active}
                  className={cn(
                    "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
                    filterTone(filter.key, active),
                  )}
                >
                  <span>{count}</span>
                  <span>{t(filter.labelKey)}</span>
                </button>
              );
            })}
            {FAILURE_FILTERS.map((filter) => {
              const active = activeFailureFilters.includes(filter.key);
              return (
                <button
                  key={filter.key}
                  type="button"
                  onClick={() => onFailureFilterChange(filter.key)}
                  aria-pressed={active}
                  className={cn(
                    "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
                    failureFilterTone(filter.key, active),
                  )}
                >
                  <span>{failureFilterCounts[filter.key]}</span>
                  <span>{t(filter.labelKey)}</span>
                </button>
              );
            })}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {hasActiveFilters ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="rounded-xl text-xs"
                onClick={onClearFilters}
              >
                <FilterX className="size-4" />
                {t("clearFilters")}
              </Button>
            ) : null}
            {manualCheckinUrls.length > 0 ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="rounded-xl text-xs"
                onClick={openAllManualCheckin}
              >
                <ExternalLink className="size-4" />
                {t("openManualCheckin", { count: manualCheckinUrls.length })}
              </Button>
            ) : null}
          </div>
        </div>

        {allTags.length > 0 ? (
          <div className="mt-3 flex flex-wrap gap-2">
            {allTags.map(({ tag, count }) => {
              const active = activeTags.includes(tag);
              return (
                <button
                  key={tag}
                  type="button"
                  onClick={() => onTagFilterChange(tag)}
                  title={
                    active
                      ? t("removeTagFilter", { tag })
                      : t("addTagFilter", { tag })
                  }
                  className={cn(
                    "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
                    active
                      ? "border-primary/30 bg-primary text-primary-foreground"
                      : "border-border bg-background text-foreground hover:bg-muted/40",
                  )}
                >
                  <Tag className="size-3" />
                  <span>{tag}</span>
                  <span className="text-[10px] opacity-70">{count}</span>
                </button>
              );
            })}
          </div>
        ) : null}
      </div>
    </section>
  );
}
