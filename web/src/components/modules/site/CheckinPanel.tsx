"use client";

import { useMemo, type ReactNode } from "react";
import { CalendarCheck2, FilterX, Globe2, KeyRound, Layers3, Sparkles } from "lucide-react";
import { type Site } from "@/api/endpoints/site";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

export type CheckinFilterStatus =
  | "all"
  | "success"
  | "failed"
  | "skipped"
  | "idle";

const FILTERS: Array<{ key: CheckinFilterStatus; label: string }> = [
  { key: "all", label: "全部" },
  { key: "success", label: "成功" },
  { key: "failed", label: "失败" },
  { key: "skipped", label: "跳过" },
  { key: "idle", label: "未执行" },
];

function sitePlatformSupportsCheckin(platform: Site["platform"]) {
  switch (platform) {
    case "done-hub":
    case "sub2api":
    case "openai":
    case "claude":
    case "gemini":
      return false;
    default:
      return true;
  }
}

function accountHasCheckinEnabled(site: Site, account: Site["accounts"][number]) {
  return sitePlatformSupportsCheckin(site.platform) && account.auto_checkin;
}

function filterTone(status: CheckinFilterStatus, active: boolean) {
  if (active) {
    switch (status) {
      case "success":
        return "border-emerald-500/30 bg-emerald-500 text-white";
      case "failed":
        return "border-destructive/30 bg-destructive text-white";
      case "skipped":
        return "border-amber-500/30 bg-amber-500 text-white";
      case "idle":
        return "border-border bg-foreground text-background";
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
    case "skipped":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "idle":
      return "border-border bg-muted/40 text-muted-foreground";
    case "all":
    default:
      return "border-border bg-background text-foreground";
  }
}

function statusLabel(status: CheckinFilterStatus) {
  const filter = FILTERS.find((item) => item.key === status);
  return filter?.label ?? "全部";
}

function OverviewMetric({
  icon,
  label,
  value,
}: {
  icon: ReactNode;
  label: string;
  value: number;
}) {
  return (
    <div className="flex items-center gap-3 rounded-2xl bg-muted/20 px-4 py-3">
      <span className="flex size-9 items-center justify-center rounded-xl bg-background text-muted-foreground shadow-sm">
        {icon}
      </span>
      <div className="min-w-0">
        <div className="text-xs text-muted-foreground">{label}</div>
        <div className="text-base font-semibold">{value}</div>
      </div>
    </div>
  );
}

export function CheckinPanel({
  sites,
  inventory,
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
    siteCount: number;
    accountCount: number;
    tokenCount: number;
    modelCount: number;
  };
  visibleSiteCount: number;
  visibleAccountCount: number;
  searchTerm: string;
  siteFilterLabel: string | null;
  hasActiveFilters: boolean;
  onClearFilters: () => void;
  filterStatus: CheckinFilterStatus;
  onFilterChange: (status: CheckinFilterStatus) => void;
}) {
  const summary = useMemo(() => {
    const counters = {
      total: 0,
      success: 0,
      failed: 0,
      skipped: 0,
      idle: 0,
    };

    for (const site of sites ?? []) {
      for (const account of site.accounts ?? []) {
        if (!accountHasCheckinEnabled(site, account)) {
          continue;
        }
        counters.total += 1;
        const status = account.last_checkin_status || "idle";
        if (status === "success") counters.success += 1;
        else if (status === "failed") counters.failed += 1;
        else if (status === "skipped") counters.skipped += 1;
        else counters.idle += 1;
      }
    }

    return counters;
  }, [sites]);

  const activeCount =
    filterStatus === "all" ? summary.total : summary[filterStatus];

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
                  : `当前按“${statusLabel(filterStatus)}”筛选，命中 ${activeCount} 个账号。`}
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
            icon={<Globe2 className="size-4" />}
            label="站点"
            value={inventory.siteCount}
          />
          <OverviewMetric
            icon={<Layers3 className="size-4" />}
            label="账号"
            value={inventory.accountCount}
          />
          <OverviewMetric
            icon={<KeyRound className="size-4" />}
            label="Key"
            value={inventory.tokenCount}
          />
          <OverviewMetric
            icon={<Sparkles className="size-4" />}
            label="模型"
            value={inventory.modelCount}
          />
        </div>

        {hasActiveFilters ? (
          <div className="mt-4 flex flex-wrap gap-2">
            {searchTerm ? <Badge variant="outline">搜索：{searchTerm}</Badge> : null}
            {siteFilterLabel ? (
              <Badge variant="outline">站点：{siteFilterLabel}</Badge>
            ) : null}
            {filterStatus !== "all" ? (
              <Badge variant="outline">签到：{statusLabel(filterStatus)}</Badge>
            ) : null}
          </div>
        ) : null}
      </div>

      <div className="px-5 py-4">
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
      </div>
    </section>
  );
}
