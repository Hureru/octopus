"use client";

import { useCallback, useMemo, type ReactNode } from "react";
import {
  AlertTriangle,
  CalendarCheck2,
  ExternalLink,
  FilterX,
  Layers3,
  TrendingUp,
  Wallet,
} from "lucide-react";
import { type Site } from "@/api/endpoints/site";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import {
  buildCheckinSummary,
  type CheckinFilterStatus,
} from "./checkin-status";

const FILTERS: Array<{ key: CheckinFilterStatus; label: string }> = [
  { key: "all", label: "全部" },
  { key: "success", label: "成功" },
  { key: "failed", label: "失败" },
  { key: "idle", label: "未执行" },
  { key: "disabled", label: "禁用" },
];

function filterTone(status: CheckinFilterStatus, active: boolean) {
  if (active) {
    switch (status) {
      case "success":
        return "border-emerald-500/30 bg-emerald-500 text-white";
      case "failed":
        return "border-destructive/30 bg-destructive text-white";
      case "idle":
        return "border-border bg-foreground text-background";
      case "disabled":
        return "border-slate-500/30 bg-slate-700 text-white dark:bg-slate-200 dark:text-slate-900";
      case "all":
      default:
        return "border-primary/30 bg-primary text-primary-foreground";
    }
  }

  switch (status) {
    case "success":
      return "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
    case "failed":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "idle":
      return "border-border bg-muted/40 text-muted-foreground";
    case "disabled":
      return "border-slate-500/20 bg-slate-500/10 text-slate-700 dark:text-slate-300";
    case "all":
    default:
      return "border-border bg-background text-foreground";
  }
}

function statusLabel(status: CheckinFilterStatus) {
  const filter = FILTERS.find((item) => item.key === status);
  return filter?.label ?? "全部";
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
  siteFilterLabel,
  hasActiveFilters,
  onClearFilters,
  filterStatus,
  onFilterChange,
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
  siteFilterLabel: string | null;
  hasActiveFilters: boolean;
  onClearFilters: () => void;
  filterStatus: CheckinFilterStatus;
  onFilterChange: (status: CheckinFilterStatus) => void;
}) {
  const summaryNow = useMemo(() => {
    const [year = "", month = "", day = ""] = statusDayKey.split("-");
    const parsed = new Date(Number(year), Number(month), Number(day));
    return Number.isNaN(parsed.getTime()) ? new Date() : parsed;
  }, [statusDayKey]);

  const summary = useMemo(
    () => buildCheckinSummary(sites, summaryNow),
    [sites, summaryNow],
  );
  const hasContextBadges = Boolean(searchTerm || siteFilterLabel);

  const manualCheckinUrls = useMemo(
    () =>
      (sites ?? [])
        .filter((s) => s.external_checkin_url?.trim())
        .map((s) => s.external_checkin_url!.trim()),
    [sites],
  );

  const openAllManualCheckin = useCallback(() => {
    for (const url of manualCheckinUrls) {
      window.open(url, "_blank");
    }
  }, [manualCheckinUrls]);

  return (
    <section className="overflow-hidden rounded-[28px] border border-border/70 bg-card shadow-[0_18px_60px_-40px_rgba(15,23,42,0.45)]">
      <div className="border-b border-border/60 bg-gradient-to-br from-background via-card to-muted/10 px-5 py-5">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div className="space-y-1">
            <div className="flex items-center gap-2 text-base font-semibold">
              <CalendarCheck2 className="size-5 text-primary" />
              <span>站点总览</span>
            </div>
            <p className="text-sm text-muted-foreground">
              {summary.total === 0
                ? "暂无启用签到的账号。"
                : filterStatus === "all"
                  ? "点击状态标签，直接定位异常站点和账号。"
                  : `当前按“${statusLabel(filterStatus)}”筛选，命中 ${visibleSiteCount} 个站点 / ${visibleAccountCount} 个账号。`}
            </p>
          </div>

          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span>当前结果</span>
            <span className="font-medium text-foreground">
              {visibleSiteCount} 站点 / {visibleAccountCount} 账号
            </span>
            {hasActiveFilters ? (
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="rounded-xl"
                onClick={onClearFilters}
              >
                <FilterX className="size-4" />
                清空筛选
              </Button>
            ) : null}
          </div>
        </div>

        <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <OverviewMetric
            icon={<Wallet className="size-4" />}
            label="当前余额"
            value={formatCurrency(inventory.totalBalance)}
          />
          <OverviewMetric
            icon={<TrendingUp className="size-4" />}
            label="累计消耗"
            value={formatCurrency(inventory.totalBalanceUsed)}
          />
          <OverviewMetric
            icon={<Layers3 className="size-4" />}
            label="启用账号"
            value={`${inventory.enabledAccounts} / ${inventory.totalAccounts}`}
          />
          <OverviewMetric
            icon={<AlertTriangle className="size-4" />}
            label="今日异常"
            value={`${summary.failed}`}
            tone={summary.failed > 0 ? "warning" : "default"}
          />
        </div>

        {hasActiveFilters && hasContextBadges ? (
          <div className="mt-4 flex flex-wrap gap-2">
            {searchTerm ? <Badge variant="outline">搜索：{searchTerm}</Badge> : null}
            {siteFilterLabel ? (
              <Badge variant="outline">站点：{siteFilterLabel}</Badge>
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
              const active = filterStatus === filter.key;
              return (
                <button
                  key={filter.key}
                  type="button"
                  onClick={() =>
                    onFilterChange(
                      active && filter.key !== "all" ? "all" : filter.key,
                    )
                  }
                  className={cn(
                    "inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-xs font-medium transition-colors",
                    filterTone(filter.key, active),
                  )}
                >
                  <span>{count}</span>
                  <span>{filter.label}</span>
                </button>
              );
            })}
          </div>
          {manualCheckinUrls.length > 0 ? (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="rounded-xl text-xs"
              onClick={openAllManualCheckin}
            >
              <ExternalLink className="size-4" />
              打开手动签到 ({manualCheckinUrls.length})
            </Button>
          ) : null}
        </div>
      </div>
    </section>
  );
}
