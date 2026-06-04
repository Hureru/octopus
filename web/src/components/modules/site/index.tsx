"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type DragEvent,
} from "react";
import { useTranslations } from "next-intl";
import { AnimatePresence, motion } from "motion/react";
import {
  Site as SiteRecord,
  SiteAccount,
  SiteCredentialType,
  SitePlatform,
  useCheckinAllSites,
  useCheckinSiteAccount,
  useArchiveSite,
  useArchivedSiteList,
  useDeleteSite,
  useDeleteSiteAccount,
  useEnableSite,
  useEnableSiteAccount,
  useImportAllAPIHub,
  useImportMetAPI,
  useRestoreSite,
  useSiteBatchAction,
  useSiteList,
  useSyncAllSites,
  useSyncSiteAccount,
  useUpdateSite,
} from "@/api/endpoints/site";
import { PageWrapper } from "@/components/common/PageWrapper";
import { toast } from "@/components/common/Toast";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/animate-ui/components/animate/tooltip";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { Switch } from "@/components/ui/switch";
import {
  useSearchStore,
  useToolbarViewOptionsStore,
} from "@/components/modules/toolbar";
import { cn } from "@/lib/utils";
import { useSettingStore } from "@/stores/setting";
import { CheckinPanel } from "./CheckinPanel";
import { SiteEditDialog } from "./SiteEditDialog";
import { AccountEditDialog } from "./AccountEditDialog";
import {
  accountHasCheckinEnabled,
  accountMatchesCheckinFilters,
  deriveCheckinStatus,
  sitePlatformSupportsCheckin,
  type CheckinFilterStatus,
} from "./checkin-status";
import { translateSiteMessage } from "./site-message";
import { useSiteUIStore } from "./ui-store";
import {
  isSiteJumpTarget,
  type PendingJump,
  type SiteJumpTarget,
  useJumpStore,
} from "@/stores/jump";
import {
  CalendarCheck2,
  CheckSquare,
  ChevronDown,
  CircleAlert,
  FileJson,
  FilterX,
  Link2,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Power,
  Plus,
  RefreshCw,
  Square,
  Archive,
  ArchiveRestore,
  Trash2,
  TriangleAlert,
  Upload,
  Waypoints,
  X,
} from "lucide-react";

const PLATFORM_LABELS: Record<SitePlatform, string> = {
  [SitePlatform.NewAPI]: "New API",
  [SitePlatform.AnyRouter]: "AnyRouter",
  [SitePlatform.OneAPI]: "One API",
  [SitePlatform.OneHub]: "One Hub",
  [SitePlatform.DoneHub]: "Done Hub",
  [SitePlatform.Sub2API]: "Sub2API",
  [SitePlatform.OpenAI]: "OpenAI",
  [SitePlatform.Claude]: "Claude",
  [SitePlatform.Gemini]: "Gemini",
};

const CREDENTIAL_LABELS: Record<SiteCredentialType, string> = {
  [SiteCredentialType.UsernamePassword]: "用户名 / 密码",
  [SiteCredentialType.AccessToken]: "Access Token",
  [SiteCredentialType.APIKey]: "API Key",
};

type HealthTone = "default" | "danger" | "muted" | "warning";

type SiteSummary = {
  accountCount: number;
  keyCount: number;
  modelCount: number;
  groupCount: number;
  balance: number;
  todayIncome: number;
  failedAccountCount: number;
  partialAccountCount: number;
  disabledAccountCount: number;
  enabledAccountCount: number;
  healthLabel: string;
  healthTone: HealthTone;
};

type VisibleSite = {
  site: SiteRecord;
  summary: SiteSummary;
  visibleAccounts: SiteAccount[];
  forceExpanded: boolean;
  hasFilteredAccounts: boolean;
};

const MENU_BUTTON_CLASS =
  "flex w-full items-center gap-2 rounded-xl px-3 py-2 text-sm text-left transition-colors hover:bg-muted/60";

type SitePendingJump = PendingJump & { target: SiteJumpTarget };
type ImportSource = "all-api-hub" | "metapi";
type SiteImportResult = {
  created_sites: number;
  reused_sites: number;
  created_accounts: number;
  updated_accounts: number;
  skipped_accounts: number;
  scheduled_sync_accounts?: number;
  warnings: string[];
  imported_tokens?: number;
  imported_groups?: number;
  imported_models?: number;
  disabled_models?: number;
};

function formatDateTime(value?: string | null) {
  if (!value) return "从未执行";
  const date = new Date(value);
  if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) {
    return "从未执行";
  }
  return date.toLocaleString();
}

function statusLabel(status: string) {
  switch (status) {
    case "partial":
      return "部分成功";
    case "success":
      return "成功";
    case "failed":
      return "失败";
    case "skipped":
      return "跳过";
    case "idle":
    default:
      return "未执行";
  }
}

function SiteMetric({
  label,
  value,
}: {
  label: string;
  value: number | string;
}) {
  return (
    <div className="rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-1 text-lg font-semibold">{value}</div>
    </div>
  );
}

function getErrorMessage(error: unknown) {
  if (error instanceof Error) return error.message;
  if (typeof error === "object" && error !== null && "message" in error) {
    const message = (error as { message?: unknown }).message;
    if (typeof message === "string") return message;
  }
  return "操作失败";
}

function getSiteErrorMessage(
  locale: ReturnType<typeof useSettingStore.getState>["locale"],
  error: unknown,
  t?: ReturnType<typeof useTranslations>,
) {
  return translateSiteMessage(locale, getErrorMessage(error), t);
}

function formatBalance(value: number) {
  if (value === 0) return "0";
  if (value >= 1000000) return `${(value / 1000000).toFixed(2)}M`;
  if (value >= 1000) return `${(value / 1000).toFixed(2)}K`;
  return value.toFixed(2);
}

function normalizeSearchTerm(value: string) {
  return value.trim().toLowerCase();
}

function matchesSearch(value: string | null | undefined, query: string) {
  return (value ?? "").toLowerCase().includes(query);
}

function normalizedStatus(status?: string | null) {
  return status || "idle";
}

function accountHasSyncFailure(account: SiteAccount) {
  return normalizedStatus(account.last_sync_status) === "failed";
}

function accountHasCheckinFailure(
  site: SiteRecord,
  account: SiteAccount,
) {
  return deriveCheckinStatus(site, account) === "failed";
}

function accountHasHealthFailure(
  site: SiteRecord,
  account: SiteAccount,
) {
  return accountHasSyncFailure(account) || accountHasCheckinFailure(site, account);
}

function statusDotClass(status: string) {
  switch (status) {
    case "success":
      return "bg-emerald-500";
    case "partial":
      return "bg-amber-500";
    case "failed":
      return "bg-destructive";
    case "skipped":
      return "bg-amber-500";
    default:
      return "bg-muted-foreground/40";
  }
}

function badgeToneClass(tone: HealthTone) {
  switch (tone) {
    case "danger":
      return "border-destructive/20 bg-destructive/10 text-destructive";
    case "muted":
      return "border-border bg-muted/40 text-muted-foreground";
    case "warning":
      return "border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300";
    case "default":
    default:
      return "border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300";
  }
}

function cardToneClass(tone: HealthTone) {
  switch (tone) {
    case "danger":
      return "border-destructive/25 bg-gradient-to-br from-destructive/[0.07] via-card to-card";
    case "muted":
      return "border-slate-400/25 bg-gradient-to-br from-slate-500/[0.06] via-card to-card dark:border-slate-600/35";
    case "warning":
      return "border-amber-500/25 bg-gradient-to-br from-amber-500/[0.07] via-card to-card";
    case "default":
    default:
      return "border-border/70 bg-card";
  }
}

function buildSiteSummary(site: SiteRecord): SiteSummary {
  let keyCount = 0;
  let modelCount = 0;
  let groupCount = 0;
  let balance = 0;
  let todayIncome = 0;
  let failedAccountCount = 0;
  let partialAccountCount = 0;
  let disabledAccountCount = 0;
  let enabledAccountCount = 0;

  for (const account of site.accounts) {
    keyCount += account.tokens.length;
    modelCount += account.models.length;
    groupCount += account.user_groups.length;
    balance += account.balance;
    todayIncome +=
      typeof account.today_income === "number" ? account.today_income : 0;

    if (account.enabled) enabledAccountCount += 1;
    else disabledAccountCount += 1;

    if (accountHasHealthFailure(site, account)) {
      failedAccountCount += 1;
    } else if (normalizedStatus(account.last_sync_status) === "partial") {
      partialAccountCount += 1;
    }
  }

  if (!site.enabled) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: "站点停用",
      healthTone: "muted",
    };
  }

  if (failedAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: `${failedAccountCount} 异常`,
      healthTone: "danger",
    };
  }

  if (disabledAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: `${disabledAccountCount} 已停用`,
      healthTone: "muted",
    };
  }

  if (partialAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: `${partialAccountCount} 部分同步`,
      healthTone: "warning",
    };
  }

  if (site.accounts.length === 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      todayIncome,
      failedAccountCount,
      partialAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: "待配置",
      healthTone: "warning",
    };
  }

  const allIdle = site.accounts.every(
    (account) =>
      account.enabled &&
      normalizedStatus(account.last_sync_status) === "idle" &&
      (!accountHasCheckinEnabled(account, site.platform) ||
        deriveCheckinStatus(site, account) === "idle"),
  );

  return {
    accountCount: site.accounts.length,
    keyCount,
    modelCount,
    groupCount,
    balance,
    todayIncome,
    failedAccountCount,
    partialAccountCount,
    disabledAccountCount,
    enabledAccountCount,
    healthLabel: allIdle ? "未执行" : "正常",
    healthTone: allIdle ? "warning" : "default",
  };
}

function CompactMetric({
  label,
  value,
}: {
  label: string;
  value: number | string;
}) {
  return (
    <span className="inline-flex items-baseline gap-1 text-xs text-muted-foreground">
      <span>{label}</span>
      <span className="font-semibold text-foreground">{value}</span>
    </span>
  );
}

function isCloudflareProtectionMessage(message?: string | null) {
  const lowered = (message ?? "").toLowerCase();
  return lowered.includes("cloudflare") || message?.includes("Cloudflare 保护") === true;
}

function ExecutionSummary({
  label,
  status,
  at,
  message,
}: {
  label: string;
  status: string;
  at?: string | null;
  message?: string | null;
}) {
  const text = [`上次${label} ${formatDateTime(at)}`, statusLabel(status)];
  if (message) {
    text.push(message);
  }

  const cloudflareProtected = isCloudflareProtectionMessage(message);
  const summary = text.join(" · ");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-start gap-2 text-xs text-muted-foreground">
          <span
            className={cn(
              "mt-1 size-2 shrink-0 rounded-full",
              cloudflareProtected ? "bg-amber-500" : statusDotClass(status),
            )}
          />
          <span className="min-w-0 truncate">
            {cloudflareProtected ? "Cloudflare 保护 · " : ""}
            {summary}
          </span>
        </div>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">{summary}</TooltipContent>
    </Tooltip>
  );
}

function StaticSummary({
  tone = "muted",
  text,
}: {
  tone?: "muted" | "warning";
  text: string;
}) {
  return (
    <div
      className={cn(
        "flex items-start gap-2 text-xs",
        tone === "warning" ? "text-amber-700 dark:text-amber-300" : "text-muted-foreground",
      )}
    >
      <span
        className={cn(
          "mt-1 size-2 shrink-0 rounded-full",
          tone === "warning" ? "bg-amber-500" : "bg-muted-foreground/40",
        )}
      />
      <span className="min-w-0 truncate">{text}</span>
    </div>
  );
}

function IconActionButton({
  label,
  className,
  ...props
}: ComponentProps<typeof Button> & { label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          size="icon-sm"
          variant="outline"
          className={cn("rounded-xl", className)}
          aria-label={label}
          title={label}
          {...props}
        />
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}

function estimateVisibleSiteCardHeight(item: VisibleSite, expanded: boolean) {
  if (item.forceExpanded || expanded) {
    return 360 + item.visibleAccounts.length * 190;
  }
  if (item.site.accounts.length === 0) {
    return 280;
  }
  return 310;
}

export function Site() {
  const t = useTranslations();
  const tProxy = useTranslations('proxyPool');
  const locale = useSettingStore((state) => state.locale);
  const { data: sites, isLoading, error } = useSiteList();
  const updateSite = useUpdateSite();
  const enableSite = useEnableSite();
  const deleteSite = useDeleteSite();
  const archiveSite = useArchiveSite();
  const restoreSite = useRestoreSite();
  const enableSiteAccount = useEnableSiteAccount();
  const deleteSiteAccount = useDeleteSiteAccount();
  const syncSiteAccount = useSyncSiteAccount();
  const checkinSiteAccount = useCheckinSiteAccount();
  const syncAllSites = useSyncAllSites();
  const checkinAllSites = useCheckinAllSites();
  const importAllAPIHub = useImportAllAPIHub();
  const importMetAPI = useImportMetAPI();
  const batchAction = useSiteBatchAction();

  const [siteDialogOpen, setSiteDialogOpen] = useState(false);
  const [importDialogOpen, setImportDialogOpen] = useState(false);
  const [archivedDialogOpen, setArchivedDialogOpen] = useState(false);
  const {
    data: archivedSites,
    isLoading: archivedLoading,
    error: archivedError,
  } = useArchivedSiteList(archivedDialogOpen);
  const [importPayloadText, setImportPayloadText] = useState("");
  const [importFile, setImportFile] = useState<File | null>(null);
  const importFileInputRef = useRef<HTMLInputElement | null>(null);
  const importDragDepthRef = useRef(0);
  const [isImportDragging, setIsImportDragging] = useState(false);
  const [importSource, setImportSource] =
    useState<ImportSource>("all-api-hub");
  const [lastImportResult, setLastImportResult] =
    useState<SiteImportResult | null>(null);
  const [editingSite, setEditingSite] = useState<SiteRecord | null>(null);

  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [accountSite, setAccountSite] = useState<SiteRecord | null>(null);
  const [editingAccount, setEditingAccount] = useState<SiteAccount | null>(
    null,
  );

  // Batch selection
  const [selectedSiteIds, setSelectedSiteIds] = useState<number[]>([]);

  // Delete confirmation
  const [deleteConfirm, setDeleteConfirm] = useState<{
    type: "site" | "account" | "archive-site";
    id: number;
    name: string;
  } | null>(null);
  const [expandedSiteIds, setExpandedSiteIds] = useState<Set<number>>(
    () => new Set(),
  );
  const [syncingAccountIds, setSyncingAccountIds] = useState<Set<number>>(
    () => new Set(),
  );
  const [checkinAccountIds, setCheckinAccountIds] = useState<Set<number>>(
    () => new Set(),
  );
  const [siteCardHeights, setSiteCardHeights] = useState<Record<number, number>>(
    {},
  );
  const [statusDayKey, setStatusDayKey] = useState(() => {
    const now = new Date();
    return `${now.getFullYear()}-${now.getMonth()}-${now.getDate()}`;
  });
  const cardObserversRef = useRef<Map<number, ResizeObserver>>(new Map());
  const cardElementsRef = useRef<Map<number, HTMLElement>>(new Map());
  const cardMeasureRefCallbacks = useRef<
    Map<number, (node: HTMLElement | null) => void>
  >(new Map());
  const accountElementsRef = useRef<Map<number, HTMLElement>>(new Map());
  const [highlightedSiteId, setHighlightedSiteId] = useState<number | null>(
    null,
  );
  const [highlightedAccountId, setHighlightedAccountId] = useState<number | null>(
    null,
  );

  const searchTerm = useSearchStore((state) => state.getSearchTerm("site"));
  const setSearchTerm = useSearchStore((state) => state.setSearchTerm);
  const siteSortField = useToolbarViewOptionsStore((state) =>
    state.getSortField("site"),
  );
  const siteSortOrder = useToolbarViewOptionsStore((state) =>
    state.getSortOrder("site"),
  );
  const checkinFilterStatuses = useSiteUIStore(
    (state) => state.checkinFilterStatuses,
  );
  const setCheckinFilterStatuses = useSiteUIStore(
    (state) => state.setCheckinFilterStatuses,
  );
  const setSiteHandlers = useSiteUIStore((state) => state.setHandlers);
  const resetSiteHandlers = useSiteUIStore((state) => state.resetHandlers);
  const pendingJump = useJumpStore((state) => state.pending);
  const clearPendingJump = useJumpStore((state) => state.clearPending);
  const requestJump = useJumpStore((state) => state.requestJump);

  const pendingSiteJump =
    pendingJump && isSiteJumpTarget(pendingJump.target)
      ? (pendingJump as SitePendingJump)
      : null;
  const forcedSiteId = pendingSiteJump?.target.siteId ?? null;

  const setSiteCardMeasureRef = useCallback(
    (siteID: number, node: HTMLElement | null) => {
      const observers = cardObserversRef.current;
      const elements = cardElementsRef.current;
      const currentNode = elements.get(siteID);

      if (currentNode === node) {
        return;
      }

      if (currentNode) {
        observers.get(siteID)?.disconnect();
        observers.delete(siteID);
        elements.delete(siteID);
      }

      if (!node) {
        return;
      }

      elements.set(siteID, node);
      const observer = new ResizeObserver((entries) => {
        const nextHeight = Math.round(
          entries[0]?.contentRect.height ?? node.getBoundingClientRect().height,
        );
        setSiteCardHeights((current) =>
          current[siteID] === nextHeight
            ? current
            : { ...current, [siteID]: nextHeight },
        );
      });
      observer.observe(node);
      observers.set(siteID, observer);

      const initialHeight = Math.round(node.getBoundingClientRect().height);
      setSiteCardHeights((current) =>
        current[siteID] === initialHeight
          ? current
          : { ...current, [siteID]: initialHeight },
      );
    },
    [],
  );

  const getSiteCardMeasureRef = useCallback(
    (siteID: number) => {
      const existing = cardMeasureRefCallbacks.current.get(siteID);
      if (existing) {
        return existing;
      }

      const callback = (node: HTMLElement | null) => {
        setSiteCardMeasureRef(siteID, node);
      };
      cardMeasureRefCallbacks.current.set(siteID, callback);
      return callback;
    },
    [setSiteCardMeasureRef],
  );

  const setAccountElementRef = useCallback(
    (accountId: number, node: HTMLElement | null) => {
      const elements = accountElementsRef.current;
      if (node) {
        elements.set(accountId, node);
        return;
      }
      elements.delete(accountId);
    },
    [],
  );

  const flashTarget = useCallback(
    (target: "site" | "account", id: number) => {
      if (target === "site") {
        setHighlightedSiteId(id);
        window.setTimeout(() => {
          setHighlightedSiteId((current) => (current === id ? null : current));
        }, 1800);
        return;
      }

      setHighlightedAccountId(id);
      window.setTimeout(() => {
        setHighlightedAccountId((current) => (current === id ? null : current));
      }, 1800);
    },
    [],
  );

  const inventory = useMemo(() => {
    let totalBalance = 0;
    let totalBalanceUsed = 0;
    let enabledAccounts = 0;
    let totalAccounts = 0;

    for (const site of sites ?? []) {
      for (const account of site.accounts) {
        totalAccounts += 1;
        if (site.enabled && account.enabled) {
          enabledAccounts += 1;
        }
        totalBalance += typeof account.balance === "number" ? account.balance : 0;
        totalBalanceUsed +=
          typeof account.balance_used === "number" ? account.balance_used : 0;
      }
    }

    return {
      totalBalance,
      totalBalanceUsed,
      enabledAccounts,
      totalAccounts,
    };
  }, [sites]);

  const normalizedQuery = useMemo(
    () => normalizeSearchTerm(searchTerm),
    [searchTerm],
  );

  const visibleSites = useMemo<VisibleSite[]>(() => {
    const hasSearch = normalizedQuery.length > 0;

    const list = (sites ?? []).flatMap((site) => {
      const summary = buildSiteSummary(site);
      const isForcedTarget = forcedSiteId === site.id;

      const hasCheckinFilters = checkinFilterStatuses.length > 0;

      const siteMatchesQuery =
        !hasSearch ||
        matchesSearch(site.name, normalizedQuery) ||
        matchesSearch(site.base_url, normalizedQuery) ||
        matchesSearch(PLATFORM_LABELS[site.platform], normalizedQuery);

      const accountMatchesQuery = (account: SiteAccount) =>
        matchesSearch(account.name, normalizedQuery);

      const matchedAccountsBySearch = hasSearch
        ? site.accounts.filter(accountMatchesQuery)
        : site.accounts;

      let visibleAccounts = site.accounts;
      let forceExpanded = hasCheckinFilters || isForcedTarget;

      if (hasCheckinFilters && !isForcedTarget) {
        visibleAccounts = visibleAccounts.filter((account) =>
          accountMatchesCheckinFilters(site, account, checkinFilterStatuses),
        );
      }

      if (hasSearch && !siteMatchesQuery && !isForcedTarget) {
        visibleAccounts = visibleAccounts.filter(accountMatchesQuery);
        forceExpanded = visibleAccounts.length > 0 || forceExpanded;
      }

      if (isForcedTarget) {
        visibleAccounts = site.accounts;
      }

      const visible =
        isForcedTarget
          ? true
          : hasCheckinFilters
            ? visibleAccounts.length > 0
            : !hasSearch || siteMatchesQuery || matchedAccountsBySearch.length > 0;

      if (!visible) {
        return [];
      }

      return [
        {
          site,
          summary,
          visibleAccounts,
          forceExpanded,
          hasFilteredAccounts: visibleAccounts.length !== site.accounts.length,
        },
      ];
    });

    if (siteSortField === "default") {
      return list;
    }

    return [...list].sort((a, b) => {
      if (a.site.is_pinned !== b.site.is_pinned) {
        return a.site.is_pinned ? -1 : 1;
      }

      let diff = 0;
      if (siteSortField === "balance") {
        diff = a.summary.balance - b.summary.balance;
      } else {
        diff = a.site.name.localeCompare(b.site.name);
      }

      if (diff !== 0) {
        return siteSortOrder === "asc" ? diff : -diff;
      }

      return a.site.sort_order - b.site.sort_order || a.site.id - b.site.id;
    });
  }, [
    sites,
    normalizedQuery,
    checkinFilterStatuses,
    forcedSiteId,
    siteSortField,
    siteSortOrder,
  ]);

  const hasActiveFilters =
    normalizedQuery.length > 0 || checkinFilterStatuses.length > 0;
  const visibleAccountCount = visibleSites.reduce(
    (sum, item) => sum + item.visibleAccounts.length,
    0,
  );

  function openCreateSiteDialog() {
    setEditingSite(null);
    setSiteDialogOpen(true);
  }

  function openEditSiteDialog(site: SiteRecord) {
    setEditingSite(site);
    setSiteDialogOpen(true);
  }

  function closeSiteDialog(open: boolean) {
    setSiteDialogOpen(open);
    if (!open) {
      setEditingSite(null);
    }
  }

  function openCreateAccountDialog(site: SiteRecord) {
    setAccountSite(site);
    setEditingAccount(null);
    setAccountDialogOpen(true);
  }

  function openEditAccountDialog(site: SiteRecord, account: SiteAccount) {
    setAccountSite(site);
    setEditingAccount(account);
    setAccountDialogOpen(true);
  }

  function closeAccountDialog(open: boolean) {
    setAccountDialogOpen(open);
    if (!open) {
      setAccountSite(null);
      setEditingAccount(null);
    }
  }

  async function handleToggleSite(site: SiteRecord) {
    try {
      await enableSite.mutateAsync({ id: site.id, enabled: !site.enabled });
      toast.success(site.enabled ? "站点已停用" : "站点已启用");
    } catch (toggleError) {
      toast.error(getSiteErrorMessage(locale, toggleError, t));
    }
  }

  async function handleDeleteSite(site: SiteRecord) {
    setDeleteConfirm({ type: "site", id: site.id, name: site.name });
  }

  async function handleArchiveSite(site: SiteRecord) {
    setDeleteConfirm({ type: "archive-site", id: site.id, name: site.name });
  }

  async function handleRestoreSite(siteId: number, siteName: string) {
    try {
      await restoreSite.mutateAsync(siteId);
      toast.success(`站点「${siteName}」已恢复，请在列表中启用`);
    } catch (err) {
      toast.error(getSiteErrorMessage(locale, err, t));
    }
  }

  async function handleToggleAccount(account: SiteAccount) {
    try {
      await enableSiteAccount.mutateAsync({
        id: account.id,
        enabled: !account.enabled,
      });
      toast.success(account.enabled ? "站点账号已停用" : "站点账号已启用");
    } catch (toggleError) {
      toast.error(getSiteErrorMessage(locale, toggleError, t));
    }
  }

  async function handleDeleteAccount(account: SiteAccount) {
    setDeleteConfirm({ type: "account", id: account.id, name: account.name });
  }

  async function handleSyncAccount(account: SiteAccount) {
    setSyncingAccountIds((current) => new Set(current).add(account.id));
    try {
      const result = await syncSiteAccount.mutateAsync(account.id);
      const summary = `${result.message}（${result.group_count} 个分组，${result.token_count} 个 Key，${result.model_count} 个模型）`;
      if (result.status === "failed") {
        toast.error(summary);
      } else if (result.status === "partial") {
        toast.warning(summary);
      } else if (result.status === "success") {
        toast.success(summary);
      } else {
        console.warn(`Unexpected site sync status: ${result.status}`);
        toast.error(summary);
      }
    } catch (syncError) {
      toast.error(translateSiteMessage(locale, getErrorMessage(syncError), t));
    } finally {
      setSyncingAccountIds((current) => {
        const next = new Set(current);
        next.delete(account.id);
        return next;
      });
    }
  }

  async function handleCheckinAccount(account: SiteAccount) {
    setCheckinAccountIds((current) => new Set(current).add(account.id));
    try {
      const result = await checkinSiteAccount.mutateAsync(account.id);
      const suffix = result.reward ? `，奖励：${result.reward}` : "";
      const message = `${statusLabel(result.status)}：${result.message}${suffix}`;
      if (result.status === "failed") {
        toast.error(message);
      } else {
        toast.success(message);
      }
    } catch (checkinError) {
      toast.error(getSiteErrorMessage(locale, checkinError, t));
    } finally {
      setCheckinAccountIds((current) => {
        const next = new Set(current);
        next.delete(account.id);
        return next;
      });
    }
  }

  async function handleImportSites() {
    const hasFile = !!importFile;
    const hasText = !!importPayloadText.trim();
    if (!hasFile && !hasText) {
      toast.error("请选择 JSON 文件或粘贴导出内容");
      return;
    }

    try {
      const payload = {
        file: importFile,
        text: importPayloadText,
      };
      const result =
        importSource === "metapi"
          ? await importMetAPI.mutateAsync(payload)
          : await importAllAPIHub.mutateAsync(payload);
      setLastImportResult(result);
      setImportFile(null);
      setImportPayloadText("");
      toast.success(
        `导入完成：新增 ${result.created_sites} 个站点，新增 ${result.created_accounts} 个账号，更新 ${result.updated_accounts} 个账号`,
      );
    } catch (importError) {
      toast.error(getSiteErrorMessage(locale, importError, t));
    }
  }

  function setSelectedImportFile(file: File | null) {
    setImportFile(file);
    setLastImportResult(null);
    setIsImportDragging(false);
    importDragDepthRef.current = 0;
    if (!file && importFileInputRef.current) {
      importFileInputRef.current.value = "";
    }
  }

  function isImportFileDrag(event: DragEvent<HTMLDivElement>) {
    return Array.from(event.dataTransfer.types).includes("Files");
  }

  function handleImportDragEnter(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    importDragDepthRef.current += 1;
    setIsImportDragging(true);
  }

  function handleImportDragLeave(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    importDragDepthRef.current = Math.max(0, importDragDepthRef.current - 1);
    if (importDragDepthRef.current === 0) {
      setIsImportDragging(false);
    }
  }

  function handleImportDragOver(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
  }

  function handleImportDrop(event: DragEvent<HTMLDivElement>) {
    if (!isImportFileDrag(event)) return;
    event.preventDefault();
    setSelectedImportFile(event.dataTransfer.files?.[0] ?? null);
  }

  async function confirmDelete() {
    if (!deleteConfirm) return;
    try {
      if (deleteConfirm.type === "site") {
        await deleteSite.mutateAsync(deleteConfirm.id);
        toast.success("站点已删除");
        setSelectedSiteIds((prev) =>
          prev.filter((id) => id !== deleteConfirm.id),
        );
        setExpandedSiteIds((current) => {
          const next = new Set(current);
          next.delete(deleteConfirm.id);
          return next;
        });
      } else if (deleteConfirm.type === "archive-site") {
        await archiveSite.mutateAsync(deleteConfirm.id);
        toast.success("站点已归档，可在『归档站点』中恢复");
        setSelectedSiteIds((prev) =>
          prev.filter((id) => id !== deleteConfirm.id),
        );
        setExpandedSiteIds((current) => {
          const next = new Set(current);
          next.delete(deleteConfirm.id);
          return next;
        });
      } else {
        await deleteSiteAccount.mutateAsync(deleteConfirm.id);
        toast.success("站点账号已删除");
      }
    } catch (deleteError) {
      toast.error(getSiteErrorMessage(locale, deleteError, t));
    }
    setDeleteConfirm(null);
  }

  function toggleSiteSelection(siteId: number) {
    setSelectedSiteIds((prev) =>
      prev.includes(siteId)
        ? prev.filter((id) => id !== siteId)
        : [...prev, siteId],
    );
  }

  async function handleBatchAction(action: string) {
    if (selectedSiteIds.length === 0) {
      toast.error("请先选择站点");
      return;
    }
    try {
      const result = await batchAction.mutateAsync({
        ids: selectedSiteIds,
        action,
      });
      const successCount = result.success_ids.length;
      const failedCount = result.failed_items.length;
      toast.success(`操作完成：成功 ${successCount}，失败 ${failedCount}`);
      if (action === "delete") {
        setSelectedSiteIds([]);
      }
    } catch (batchError) {
      toast.error(getSiteErrorMessage(locale, batchError, t));
    }
  }

  async function handleTogglePin(site: SiteRecord) {
    try {
      await updateSite.mutateAsync({ id: site.id, is_pinned: !site.is_pinned });
      toast.success(site.is_pinned ? "已取消置顶" : "已置顶");
    } catch (pinError) {
      toast.error(getSiteErrorMessage(locale, pinError, t));
    }
  }

  function handleCheckinFilterChange(status: CheckinFilterStatus) {
    if (status === "all") {
      setCheckinFilterStatuses([]);
      return;
    }

    setCheckinFilterStatuses((current) =>
      current.includes(status)
        ? current.filter((item) => item !== status)
        : [...current, status],
    );
  }

  function clearFilters() {
    setSearchTerm("site", "");
    setCheckinFilterStatuses([]);
  }

  function jumpToSiteChannel(siteId: number) {
    requestJump({ kind: "site-channel-card", siteId });
  }

  function jumpToSiteChannelAccount(siteId: number, accountId: number) {
    requestJump({ kind: "site-channel-account", siteId, accountId });
  }

  function toggleSiteExpanded(siteId: number, forceExpanded: boolean) {
    if (forceExpanded) return;
    setExpandedSiteIds((current) => {
      const next = new Set(current);
      if (next.has(siteId)) next.delete(siteId);
      else next.add(siteId);
      return next;
    });
  }

  useEffect(() => {
    setSiteHandlers({
      openCreateDialog: () => {
        setEditingSite(null);
        setSiteDialogOpen(true);
      },
      openImportDialog: () => setImportDialogOpen(true),
      openArchivedDialog: () => setArchivedDialogOpen(true),
      syncAll: () => {
        syncAllSites.mutate(undefined, {
          onSuccess: () => toast.success("已触发后台全量同步，页面会自动刷新"),
          onError: (error) => toast.error(getSiteErrorMessage(locale, error, t)),
        });
      },
      checkinAll: () => {
        checkinAllSites.mutate(undefined, {
          onSuccess: () => toast.success("已触发后台全量签到，页面会自动刷新"),
          onError: (error) => toast.error(getSiteErrorMessage(locale, error, t)),
        });
      },
    });

    return () => {
      resetSiteHandlers();
    };
  }, [setSiteHandlers, resetSiteHandlers, syncAllSites, checkinAllSites, locale, t]);

  useEffect(() => {
    const updateDayKey = () => {
      const now = new Date();
      setStatusDayKey(`${now.getFullYear()}-${now.getMonth()}-${now.getDate()}`);
    };

    updateDayKey();
    const timer = window.setInterval(updateDayKey, 60_000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    const observerMap = cardObserversRef.current;
    const elementMap = cardElementsRef.current;
    const callbackMap = cardMeasureRefCallbacks.current;
    const accountMap = accountElementsRef.current;
    return () => {
      for (const observer of observerMap.values()) {
        observer.disconnect();
      }
      observerMap.clear();
      elementMap.clear();
      callbackMap.clear();
      accountMap.clear();
    };
  }, []);

  useEffect(() => {
    if (!pendingSiteJump) return;

    const { requestId, target } = pendingSiteJump;
    const targetSiteId = target.siteId;
    const siteVisible = visibleSites.some((item) => item.site.id === targetSiteId);
    if (!siteVisible) return;

    if (target.kind === "site-account") {
      setExpandedSiteIds((current) => {
        if (current.has(target.siteId)) return current;
        const next = new Set(current);
        next.add(target.siteId);
        return next;
      });
    }

    const node =
      target.kind === "site-account"
        ? accountElementsRef.current.get(target.accountId)
        : cardElementsRef.current.get(target.siteId);
    if (!node) return;

    const timer = window.setTimeout(() => {
      node.scrollIntoView({ behavior: "smooth", block: "center" });
      flashTarget("site", target.siteId);
      if (target.kind === "site-account") {
        flashTarget("account", target.accountId);
      }
      clearPendingJump(requestId);
    }, 80);

    return () => window.clearTimeout(timer);
  }, [pendingSiteJump, visibleSites, clearPendingJump, flashTarget]);

  const masonryColumns = useMemo<[VisibleSite[], VisibleSite[]]>(() => {
    const left: VisibleSite[] = [];
    const right: VisibleSite[] = [];
    let leftHeight = 0;
    let rightHeight = 0;

    for (const item of visibleSites) {
      const isExpanded = item.forceExpanded || expandedSiteIds.has(item.site.id);
      const estimatedHeight =
        siteCardHeights[item.site.id] ??
        estimateVisibleSiteCardHeight(item, isExpanded);
      if (leftHeight <= rightHeight) {
        left.push(item);
        leftHeight += estimatedHeight;
      } else {
        right.push(item);
        rightHeight += estimatedHeight;
      }
    }

    return [left, right];
  }, [visibleSites, expandedSiteIds, siteCardHeights]);

  const renderSiteCard = ({
    site,
    summary,
    visibleAccounts,
    forceExpanded,
    hasFilteredAccounts,
  }: VisibleSite) => {
    const isExpanded = forceExpanded || expandedSiteIds.has(site.id);

    return (
      <section
        key={site.id}
        className={cn(
          "rounded-[28px] border bg-card p-5 shadow-[0_20px_60px_-42px_rgba(15,23,42,0.45)] transition-colors",
          cardToneClass(summary.healthTone),
          highlightedSiteId === site.id &&
            "ring-2 ring-primary/35 ring-offset-2 ring-offset-background",
        )}
      >
        <div className="flex items-start gap-3">
          <button
            type="button"
            className="mt-1 shrink-0 text-muted-foreground transition-colors hover:text-foreground"
            title={
              selectedSiteIds.includes(site.id) ? "取消选择站点" : "选择站点"
            }
            onClick={() => toggleSiteSelection(site.id)}
          >
            {selectedSiteIds.includes(site.id) ? (
              <CheckSquare className="size-5 text-primary" />
            ) : (
              <Square className="size-5" />
            )}
          </button>

          <div className="min-w-0 flex-1">
            <div className="flex items-start gap-3">
              <div
                className="min-w-0 flex-1 cursor-pointer text-left"
                role="button"
                tabIndex={0}
                onClick={() => toggleSiteExpanded(site.id, forceExpanded)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    toggleSiteExpanded(site.id, forceExpanded);
                  }
                }}
              >
                <div className="flex flex-wrap items-center gap-2">
                  <h2 className="truncate text-lg font-semibold">{site.name}</h2>
                  {site.is_pinned ? (
                    <Badge variant="outline" className="text-amber-600">
                      <Pin className="mr-1 size-3" />
                      置顶
                    </Badge>
                  ) : null}
                  <Badge variant="outline">
                    {PLATFORM_LABELS[site.platform]}
                  </Badge>
                  <Badge
                    variant="outline"
                    className={badgeToneClass(summary.healthTone)}
                  >
                    {summary.healthLabel}
                  </Badge>
                </div>

                <div className="mt-2 flex items-center gap-2 text-sm text-muted-foreground">
                  <Link2 className="size-4 shrink-0" />
                  <a
                    href={site.base_url}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="truncate hover:text-foreground hover:underline transition-colors"
                    onClick={(e) => e.stopPropagation()}
                  >
                    {site.base_url}
                  </a>
                </div>

                <div className="mt-3 flex flex-wrap gap-x-4 gap-y-2">
                  <CompactMetric label="账号" value={summary.accountCount} />
                  <CompactMetric label="Key" value={summary.keyCount} />
                  <CompactMetric label="模型" value={summary.modelCount} />
                  <CompactMetric label="余额" value={formatBalance(summary.balance)} />
                  <CompactMetric
                    label="今日收入"
                    value={formatBalance(summary.todayIncome)}
                  />
                </div>

                <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span>
                    {site.proxy_mode === "pool"
                      ? tProxy('mode.pool')
                      : site.proxy_mode === "system"
                        ? tProxy('mode.system')
                        : tProxy('mode.direct')}
                  </span>
                  {site.custom_header.length > 0 ? (
                    <span>{site.custom_header.length} 个 Header</span>
                  ) : null}
                  {site.external_checkin_url ? <span>手动签到</span> : null}
                </div>
              </div>

              <div className="flex items-center gap-1">
                {site.accounts.length === 0 ? (
                  <IconActionButton
                    label="新增账号"
                    onClick={() => openCreateAccountDialog(site)}
                  >
                    <Plus className="size-4" />
                  </IconActionButton>
                ) : null}

                <Popover>
                  <PopoverTrigger asChild>
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="outline"
                      className="rounded-xl"
                      aria-label="更多站点操作"
                      title="更多站点操作"
                    >
                      <MoreHorizontal className="size-4" />
                    </Button>
                  </PopoverTrigger>
                  <PopoverContent
                    align="end"
                    className="w-52 rounded-2xl border border-border/60 bg-card p-2"
                  >
                    <div className="grid gap-1">
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => jumpToSiteChannel(site.id)}
                      >
                        <Waypoints className="size-4" />
                        <span>查看站点渠道</span>
                      </button>
                      {site.accounts.length > 0 ? (
                        <button
                          type="button"
                          className={MENU_BUTTON_CLASS}
                          onClick={() => openCreateAccountDialog(site)}
                        >
                          <Plus className="size-4" />
                          <span>新增账号</span>
                        </button>
                      ) : null}
                      <div className="my-1 border-t border-border/60" />
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => openEditSiteDialog(site)}
                      >
                        <Pencil className="size-4" />
                        <span>编辑站点</span>
                      </button>
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => handleTogglePin(site)}
                      >
                        {site.is_pinned ? (
                          <PinOff className="size-4" />
                        ) : (
                          <Pin className="size-4" />
                        )}
                        <span>{site.is_pinned ? "取消置顶" : "置顶"}</span>
                      </button>
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => handleToggleSite(site)}
                      >
                        <Power className="size-4" />
                        <span>{site.enabled ? "停用站点" : "启用站点"}</span>
                      </button>
                      <button
                        type="button"
                        className={MENU_BUTTON_CLASS}
                        onClick={() => handleArchiveSite(site)}
                      >
                        <Archive className="size-4" />
                        <span>归档站点</span>
                      </button>
                      <button
                        type="button"
                        className={cn(MENU_BUTTON_CLASS, "text-destructive")}
                        onClick={() => handleDeleteSite(site)}
                      >
                        <Trash2 className="size-4" />
                        <span>删除站点</span>
                      </button>
                    </div>
                  </PopoverContent>
                </Popover>

                <IconActionButton
                  label={
                    forceExpanded
                      ? "筛选结果已自动展开"
                      : isExpanded
                        ? "收起账号"
                        : "展开账号"
                  }
                  disabled={forceExpanded || site.accounts.length === 0}
                  onClick={() => toggleSiteExpanded(site.id, forceExpanded)}
                >
                  <ChevronDown
                    className={cn(
                      "size-4 transition-transform",
                      isExpanded && "rotate-180",
                    )}
                  />
                </IconActionButton>
              </div>
            </div>

            <AnimatePresence initial={false}>
              {isExpanded ? (
                <motion.div
                  key="site-accounts"
                  initial={{ height: 0, opacity: 0 }}
                  animate={{ height: "auto", opacity: 1 }}
                  exit={{ height: 0, opacity: 0 }}
                  transition={{ duration: 0.22, ease: "easeOut" }}
                  className="overflow-hidden"
                >
                  <div className="mt-4 border-t border-border/60 pt-4">
                    {hasFilteredAccounts ? (
                      <div className="mb-3 text-xs text-muted-foreground">
                        显示 {visibleAccounts.length} / {site.accounts.length} 个账号
                      </div>
                    ) : null}

                    {visibleAccounts.length === 0 ? (
                      <div className="rounded-2xl border border-dashed border-border/70 bg-muted/10 px-4 py-6 text-sm text-muted-foreground">
                        暂无账号。添加账号后即可自动同步分组、模型和渠道绑定。
                      </div>
                    ) : (
                      <div className="space-y-2">
                        {visibleAccounts.map((account) => {
                          const accountFailed = accountHasHealthFailure(site, account);
                          const accountTone: HealthTone = accountFailed
                            ? "danger"
                            : account.enabled
                              ? "default"
                              : "muted";
                          const supportsCheckin = sitePlatformSupportsCheckin(
                            site.platform,
                          );
                          const canShowManualCheckin =
                            supportsCheckin &&
                            accountHasCheckinEnabled(account, site.platform);

                          return (
                            <article
                              key={account.id}
                              ref={(node) => setAccountElementRef(account.id, node)}
                              className={cn(
                                "rounded-2xl border px-4 py-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.04)] transition-colors",
                                cardToneClass(accountTone),
                                highlightedAccountId === account.id &&
                                  "ring-2 ring-primary/35 ring-offset-2 ring-offset-background",
                              )}
                            >
                              <div className="space-y-3">
                                <div className="flex items-start gap-3">
                                  <div className="min-w-0 flex-1 space-y-2">
                                    <div className="flex flex-wrap items-center gap-2">
                                      <div className="text-sm font-semibold">
                                        {account.name}
                                      </div>
                                      <Badge variant="outline">
                                        {
                                          CREDENTIAL_LABELS[
                                            account.credential_type
                                          ]
                                        }
                                      </Badge>
                                      <Badge
                                        variant="outline"
                                        className={
                                          account.enabled
                                            ? "text-emerald-600"
                                            : "text-muted-foreground"
                                        }
                                      >
                                        {account.enabled ? "启用中" : "已停用"}
                                      </Badge>
                                    </div>

                                    <div className="flex flex-wrap gap-x-4 gap-y-1">
                                      <CompactMetric
                                        label="分组"
                                        value={account.user_groups.length}
                                      />
                                      <CompactMetric
                                        label="模型"
                                        value={account.models.length}
                                      />
                                      <CompactMetric
                                        label="余额"
                                        value={formatBalance(account.balance)}
                                      />
                                      <CompactMetric
                                        label="今日收入"
                                        value={formatBalance(account.today_income)}
                                      />
                                    </div>

                                    <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                                      <span>
                                        {account.auto_sync ? "自动同步" : "手动同步"}
                                      </span>
                                      <span>
                                        {account.auto_checkin
                                          ? account.random_checkin
                                            ? "随机签到"
                                            : "自动签到"
                                          : "手动签到"}
                                      </span>
                                      <span>
                                        {account.proxy_mode === "inherit"
                                          ? tProxy('site.inherit')
                                          : account.proxy_mode === "pool"
                                            ? tProxy('mode.pool')
                                            : account.proxy_mode === "system"
                                              ? tProxy('mode.system')
                                              : tProxy('mode.direct')}
                                      </span>
                                    </div>
                                  </div>

                                  <div className="flex shrink-0 items-center gap-2 self-start">
                                    <Tooltip>
                                      <TooltipTrigger asChild>
                                        <span>
                                          <Switch
                                            checked={account.enabled}
                                            disabled={enableSiteAccount.isPending}
                                            onCheckedChange={() =>
                                              handleToggleAccount(account)
                                            }
                                          />
                                        </span>
                                      </TooltipTrigger>
                                      <TooltipContent>
                                        {account.enabled ? "停用账号" : "启用账号"}
                                      </TooltipContent>
                                    </Tooltip>

                                    <IconActionButton
                                      label="同步账号"
                                      disabled={syncingAccountIds.has(account.id)}
                                      onClick={() => handleSyncAccount(account)}
                                    >
                                      <RefreshCw
                                        className={cn(
                                          "size-4",
                                          syncingAccountIds.has(account.id) &&
                                            "animate-spin",
                                        )}
                                      />
                                    </IconActionButton>

                                    <Popover>
                                      <PopoverTrigger asChild>
                                        <Button
                                          type="button"
                                          size="icon-sm"
                                          variant="outline"
                                          className="rounded-xl"
                                          aria-label="更多账号操作"
                                          title="更多账号操作"
                                        >
                                          <MoreHorizontal className="size-4" />
                                        </Button>
                                      </PopoverTrigger>
                                      <PopoverContent
                                        align="end"
                                        className="w-44 rounded-2xl border border-border/60 bg-card p-2"
                                      >
                                        <div className="grid gap-1">
                                          <button
                                            type="button"
                                            className={MENU_BUTTON_CLASS}
                                            onClick={() =>
                                              jumpToSiteChannelAccount(site.id, account.id)
                                            }
                                          >
                                            <Waypoints className="size-4" />
                                            <span>查看站点渠道</span>
                                          </button>
                                          <button
                                            type="button"
                                            className={cn(
                                              MENU_BUTTON_CLASS,
                                              "disabled:cursor-not-allowed disabled:opacity-50",
                                            )}
                                            onClick={() =>
                                              handleCheckinAccount(account)
                                            }
                                            disabled={checkinAccountIds.has(account.id)}
                                            hidden={!canShowManualCheckin}
                                          >
                                            <CalendarCheck2 className="size-4" />
                                            <span>立即签到</span>
                                          </button>
                                          <button
                                            type="button"
                                            className={MENU_BUTTON_CLASS}
                                            onClick={() =>
                                              openEditAccountDialog(site, account)
                                            }
                                          >
                                            <Pencil className="size-4" />
                                            <span>编辑账号</span>
                                          </button>
                                          <button
                                            type="button"
                                            className={cn(
                                              MENU_BUTTON_CLASS,
                                              "text-destructive",
                                            )}
                                            onClick={() =>
                                              handleDeleteAccount(account)
                                            }
                                          >
                                            <Trash2 className="size-4" />
                                            <span>删除账号</span>
                                          </button>
                                        </div>
                                      </PopoverContent>
                                    </Popover>
                                  </div>
                                </div>

                                <div className="space-y-1">
                                    <ExecutionSummary
                                      label="同步"
                                      status={normalizedStatus(
                                        account.last_sync_status,
                                      )}
                                      at={account.last_sync_at}
                                      message={
                                        translateSiteMessage(locale, account.last_sync_message, t) || "等待首次同步"
                                      }
                                    />
                                    {supportsCheckin ? (
                                      accountHasCheckinEnabled(
                                        account,
                                        site.platform,
                                      ) ? (
                                        <ExecutionSummary
                                          label="签到"
                                          status={normalizedStatus(
                                            account.last_checkin_status,
                                          )}
                                          at={account.last_checkin_at}
                                          message={
                                            account.last_checkin_message ||
                                            "等待首次签到"
                                          }
                                        />
                                      ) : (
                                        <StaticSummary text="签到未启用" />
                                      )
                                    ) : (
                                      <StaticSummary
                                        tone="warning"
                                        text="当前平台不支持签到"
                                      />
                                    )}
                                    {account.auto_checkin &&
                                    account.random_checkin ? (
                                      <div className="pl-4 text-xs text-muted-foreground">
                                        下次自动签到{" "}
                                        {account.next_auto_checkin_at
                                          ? formatDateTime(
                                              account.next_auto_checkin_at,
                                            )
                                          : "待调度"}{" "}
                                        · 最小间隔 {account.checkin_interval_hours} 小时 ·
                                        随机延迟 0-
                                        {account.checkin_random_window_minutes} 分钟
                                      </div>
                                    ) : null}
                                </div>
                              </div>
                            </article>
                          );
                        })}
                      </div>
                    )}
                  </div>
                </motion.div>
              ) : null}
            </AnimatePresence>
          </div>
        </div>
      </section>
    );
  };

  return (
    <div className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-3xl">
      <PageWrapper
        className="space-y-4 pb-24 md:pb-4"
        childLayout={false}
      >
        <CheckinPanel
          sites={sites}
          inventory={inventory}
          statusDayKey={statusDayKey}
          visibleSiteCount={visibleSites.length}
          visibleAccountCount={visibleAccountCount}
          searchTerm={searchTerm.trim()}
          hasActiveFilters={hasActiveFilters}
          onClearFilters={clearFilters}
          activeFilterStatuses={checkinFilterStatuses}
          onFilterChange={handleCheckinFilterChange}
        />

        {selectedSiteIds.length > 0 ? (
          <section className="rounded-3xl border border-primary/30 bg-primary/5 p-4">
            <div className="flex flex-wrap items-center gap-3">
              {(() => {
                const visibleIds = visibleSites.map((item) => item.site.id);
                const allVisibleSelected =
                  visibleIds.length > 0 &&
                  visibleIds.every((id) => selectedSiteIds.includes(id));
                return (
                  <button
                    type="button"
                    onClick={() => {
                      if (allVisibleSelected) {
                        setSelectedSiteIds((prev) =>
                          prev.filter((id) => !visibleIds.includes(id))
                        );
                      } else {
                        setSelectedSiteIds((prev) =>
                          Array.from(new Set([...prev, ...visibleIds]))
                        );
                      }
                    }}
                    disabled={visibleIds.length === 0}
                    title={allVisibleSelected ? "取消全选" : "全选当前可见站点"}
                    className="inline-flex items-center gap-2 text-sm font-medium text-foreground transition-colors hover:text-primary disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {allVisibleSelected ? (
                      <CheckSquare className="size-5 text-primary" />
                    ) : (
                      <Square className="size-5" />
                    )}
                    全选
                  </button>
                );
              })()}
              <span className="text-sm font-medium">
                已选 {selectedSiteIds.length} 个站点
              </span>
              <Button
                variant="outline"
                size="sm"
                className="rounded-xl"
                onClick={() => handleBatchAction("enable")}
                disabled={batchAction.isPending}
              >
                批量启用
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="rounded-xl"
                onClick={() => handleBatchAction("disable")}
                disabled={batchAction.isPending}
              >
                批量禁用
              </Button>
              <Button
                variant="destructive"
                size="sm"
                className="rounded-xl"
                onClick={() => handleBatchAction("delete")}
                disabled={batchAction.isPending}
              >
                批量删除
              </Button>
              <Button
                variant="ghost"
                size="sm"
                className="rounded-xl"
                onClick={() => setSelectedSiteIds([])}
              >
                取消选择
              </Button>
            </div>
          </section>
        ) : null}

        {error ? (
          <section className="rounded-3xl border border-destructive/30 bg-destructive/5 p-6 text-sm text-destructive">
            站点列表加载失败：{getSiteErrorMessage(locale, error, t)}
          </section>
        ) : null}

        {isLoading ? (
          <section className="rounded-3xl border border-border bg-card p-6 text-sm text-muted-foreground">
            正在加载站点信息...
          </section>
        ) : null}

        {!isLoading && !error && (!sites || sites.length === 0) ? (
          <section className="rounded-3xl border border-dashed border-border bg-card p-10 text-center">
            <CircleAlert className="mx-auto size-8 text-muted-foreground" />
            <div className="mt-4 text-lg font-semibold">还没有站点</div>
            <p className="mt-2 text-sm text-muted-foreground">
              先新增一个站点，再为它配置账号，后续即可自动同步分组、模型和托管渠道。
            </p>
            <Button onClick={openCreateSiteDialog} className="mt-5 rounded-xl">
              <Plus className="size-4" />
              新增第一个站点
            </Button>
          </section>
        ) : null}

        {!isLoading &&
        !error &&
        sites &&
        sites.length > 0 &&
        visibleSites.length === 0 ? (
          <section className="rounded-3xl border border-dashed border-border bg-card p-10 text-center">
            <CircleAlert className="mx-auto size-8 text-muted-foreground" />
            <div className="mt-4 text-lg font-semibold">没有匹配的站点</div>
            <p className="mt-2 text-sm text-muted-foreground">
              当前搜索和筛选条件没有命中任何站点或账号。
            </p>
            <Button
              type="button"
              variant="outline"
              className="mt-5 rounded-xl"
              onClick={clearFilters}
            >
              <FilterX className="size-4" />
              清空筛选
            </Button>
          </section>
        ) : null}

        {visibleSites.length > 0 ? (
          <>
            <div className="space-y-4 md:hidden">
              {visibleSites.map((item) => (
                <div
                  key={item.site.id}
                  ref={getSiteCardMeasureRef(item.site.id)}
                >
                  {renderSiteCard(item)}
                </div>
              ))}
            </div>
            <div className="hidden items-start gap-4 md:grid md:grid-cols-2">
              <div className="space-y-4">
                {masonryColumns[0].map((item) => (
                  <div
                    key={item.site.id}
                    ref={getSiteCardMeasureRef(item.site.id)}
                  >
                    {renderSiteCard(item)}
                  </div>
                ))}
              </div>
              <div className="space-y-4">
                {masonryColumns[1].map((item) => (
                  <div
                    key={item.site.id}
                    ref={getSiteCardMeasureRef(item.site.id)}
                  >
                    {renderSiteCard(item)}
                  </div>
                ))}
              </div>
            </div>
          </>
        ) : null}
      </PageWrapper>

      <SiteEditDialog
        key={editingSite ? `edit-site-${editingSite.id}` : "create-site"}
        open={siteDialogOpen}
        onOpenChange={closeSiteDialog}
        site={editingSite}
        onCreated={(createdSite) => openCreateAccountDialog(createdSite)}
      />

      <AccountEditDialog
        key={
          editingAccount
            ? `edit-site-account-${editingAccount.id}`
            : accountSite
              ? `create-site-account-${accountSite.id}`
              : "site-account"
        }
        open={accountDialogOpen}
        onOpenChange={closeAccountDialog}
        site={accountSite}
        account={editingAccount}
      />

      <Dialog
        open={importDialogOpen}
        onOpenChange={(open) => {
          setImportDialogOpen(open);
          if (!open) setLastImportResult(null);
        }}
      >
        <DialogContent className="max-w-3xl rounded-3xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <FileJson className="size-5" />
              导入站点数据
            </DialogTitle>
            <DialogDescription>
              支持上传或粘贴 All API Hub / Metapi 导出的 JSON。导入会按平台和站点地址自动创建或复用站点。
            </DialogDescription>
          </DialogHeader>

          <div
            className="space-y-5"
            onDragEnter={handleImportDragEnter}
            onDragLeave={handleImportDragLeave}
            onDragOver={handleImportDragOver}
            onDrop={handleImportDrop}
          >
            <div className="grid gap-2 text-sm">
              <span className="font-medium">导入来源</span>
              <Select
                value={importSource}
                onValueChange={(value) => {
                  setImportSource(value as ImportSource);
                  setLastImportResult(null);
                }}
              >
                <SelectTrigger className="rounded-xl">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all-api-hub">All API Hub</SelectItem>
                  <SelectItem value="metapi">Metapi</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="grid gap-2 text-sm">
              <div className="text-sm font-medium">上传 JSON 文件</div>
              <div className="flex items-center gap-2">
                <Input
                  ref={importFileInputRef}
                  type="file"
                  accept=".json,application/json"
                  onChange={(event) => {
                    setSelectedImportFile(event.target.files?.[0] ?? null);
                  }}
                  className="hidden"
                />
                <button
                  type="button"
                  onClick={() => importFileInputRef.current?.click()}
                  className={cn(
                    "flex min-w-0 flex-1 items-center justify-center rounded-xl border border-dashed px-3 text-center text-sm transition-all hover:bg-muted/30",
                    isImportDragging
                      ? "min-h-28 border-primary bg-primary/10 text-primary"
                      : "min-h-10 border-border bg-muted/20",
                  )}
                >
                  <span
                    className={cn(
                      "min-w-0 truncate",
                      importFile ? "text-foreground" : "text-muted-foreground",
                    )}
                  >
                    {isImportDragging
                      ? "松开即可上传 JSON 文件"
                      : importFile?.name ?? "点击选择或拖拽 JSON 文件到这里"}
                  </span>
                </button>
                <IconActionButton
                  label="清除文件"
                  onClick={() => {
                    setSelectedImportFile(null);
                  }}
                  disabled={!importFile}
                  className={!importFile ? "opacity-50" : undefined}
                >
                  <X className="size-4" />
                </IconActionButton>
              </div>
              <div className="text-xs text-muted-foreground">
                {importFile
                  ? `已选择：${importFile.name}`
                  : `支持 ${importSource === "metapi" ? "Metapi" : "All API Hub"} 导出的 .json 文件`}
              </div>
            </div>

            <label className="grid gap-2 text-sm">
              <span className="font-medium">或粘贴导出 JSON</span>
              <textarea
                value={importPayloadText}
                onChange={(event) => {
                  setImportPayloadText(event.target.value);
                  setLastImportResult(null);
                }}
                placeholder={
                  importSource === "metapi"
                    ? '粘贴类似 {"version":"2.1","accounts":{"sites":[...],"accounts":[...]}} 的完整导出内容'
                    : '粘贴类似 {"accounts":{"accounts":[...]}} 的完整导出内容'
                }
                className="min-h-40 rounded-2xl border border-input bg-background px-4 py-3 font-mono text-xs outline-none transition focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/20"
              />
              <span className="text-xs text-muted-foreground">
                {importSource === "metapi"
                  ? "Metapi 导入只迁移站点、账号、Key、分组和模型；路由策略与下游 Key 会跳过。"
                  : "导入会保留已存在站点的本地配置；同一分组下的多个 key 后续仍会聚合到同一个托管 channel。"}
              </span>
            </label>

            {lastImportResult ? (
              <div className="space-y-4 rounded-2xl border border-border/60 bg-muted/10 p-4">
                <div className="grid gap-3 sm:grid-cols-3">
                  <SiteMetric
                    label="新增站点"
                    value={lastImportResult.created_sites}
                  />
                  <SiteMetric
                    label="复用站点"
                    value={lastImportResult.reused_sites}
                  />
                  <SiteMetric
                    label="新增账号"
                    value={lastImportResult.created_accounts}
                  />
                  <SiteMetric
                    label="更新账号"
                    value={lastImportResult.updated_accounts}
                  />
                  <SiteMetric
                    label="跳过账号"
                    value={lastImportResult.skipped_accounts}
                  />
                  {typeof lastImportResult.scheduled_sync_accounts ===
                  "number" ? (
                    <SiteMetric
                      label="后台同步"
                      value={lastImportResult.scheduled_sync_accounts}
                    />
                  ) : null}
                  {typeof lastImportResult.imported_tokens === "number" ? (
                    <>
                      <SiteMetric
                        label="导入 Key"
                        value={lastImportResult.imported_tokens}
                      />
                      <SiteMetric
                        label="导入分组"
                        value={lastImportResult.imported_groups ?? 0}
                      />
                      <SiteMetric
                        label="导入模型"
                        value={lastImportResult.imported_models ?? 0}
                      />
                      <SiteMetric
                        label="禁用模型"
                        value={lastImportResult.disabled_models ?? 0}
                      />
                    </>
                  ) : null}
                </div>

                {lastImportResult.warnings.length > 0 ? (
                  <div className="rounded-2xl border border-border/60 bg-background/70 p-4">
                    <div className="flex items-center gap-2 text-sm font-medium">
                      <TriangleAlert className="size-4 text-muted-foreground" />
                      <span>导入告警</span>
                    </div>
                    <div className="mt-3 space-y-2 text-sm text-muted-foreground">
                      {lastImportResult.warnings.map((warning) => (
                        <div
                          key={warning}
                          className="break-all rounded-xl border border-border/60 bg-muted/20 px-3 py-2"
                        >
                          {warning}
                        </div>
                      ))}
                    </div>
                  </div>
                ) : null}
              </div>
            ) : null}
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              className="rounded-xl"
              onClick={() => setImportDialogOpen(false)}
            >
              关闭
            </Button>
            <Button
              onClick={handleImportSites}
              disabled={importAllAPIHub.isPending || importMetAPI.isPending}
              className="rounded-xl"
            >
              <Upload
                className={cn(
                  "size-4",
                  importAllAPIHub.isPending || importMetAPI.isPending
                    ? "animate-pulse"
                    : "",
                )}
              />
              {importAllAPIHub.isPending || importMetAPI.isPending
                ? "导入中..."
                : "开始导入"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={archivedDialogOpen} onOpenChange={setArchivedDialogOpen}>
        <DialogContent className="flex h-[min(85vh,42rem)] max-w-3xl flex-col overflow-hidden rounded-3xl border-border/70 p-0 sm:max-w-3xl">
          <DialogHeader className="shrink-0 border-b border-border/60 px-6 py-4">
            <DialogTitle>归档站点</DialogTitle>
            <DialogDescription>
              归档的站点仍保留账号、Key 和模型配置，托管渠道会被下线。点击恢复会还原到主列表（默认保持禁用状态，启用后会自动重建托管渠道）。
            </DialogDescription>
          </DialogHeader>
          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-4">
            {archivedLoading ? (
              <div className="py-10 text-center text-sm text-muted-foreground">
                正在加载归档站点...
              </div>
            ) : archivedError ? (
              <div className="rounded-2xl border border-destructive/30 bg-destructive/5 p-4 text-sm text-destructive">
                加载失败：{getSiteErrorMessage(locale, archivedError, t)}
              </div>
            ) : !archivedSites || archivedSites.length === 0 ? (
              <div className="py-10 text-center text-sm text-muted-foreground">
                当前没有归档的站点。
              </div>
            ) : (
              <div className="space-y-2">
                {archivedSites.map((site) => (
                  <div
                    key={site.id}
                    className="flex flex-wrap items-center gap-3 rounded-2xl border border-border bg-card/60 p-3"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="truncate font-medium">
                          {site.name}
                        </span>
                        <Badge variant="outline" className="rounded-full text-xs">
                          {site.platform}
                        </Badge>
                        <span className="truncate text-xs text-muted-foreground">
                          {site.base_url}
                        </span>
                      </div>
                      <div className="mt-1 text-xs text-muted-foreground">
                        归档于{" "}
                        {site.archived_at
                          ? new Date(site.archived_at).toLocaleString()
                          : "-"}
                        {" · "}
                        {site.accounts.length} 个账号已保留
                      </div>
                    </div>
                    <Button
                      variant="outline"
                      size="sm"
                      className="rounded-xl"
                      onClick={() => handleRestoreSite(site.id, site.name)}
                      disabled={restoreSite.isPending}
                    >
                      <ArchiveRestore className="size-4" />
                      恢复
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
          <DialogFooter className="shrink-0 border-t border-border/60 px-6 py-4">
            <Button
              variant="outline"
              className="rounded-xl"
              onClick={() => setArchivedDialogOpen(false)}
            >
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={!!deleteConfirm}
        onOpenChange={(open) => {
          if (!open) setDeleteConfirm(null);
        }}
      >
        <DialogContent className="max-w-md rounded-3xl">
          <DialogHeader>
            <DialogTitle>
              {deleteConfirm?.type === "archive-site" ? "确认归档" : "确认删除"}
            </DialogTitle>
            <DialogDescription>
              {deleteConfirm?.type === "site"
                ? `确认删除站点「${deleteConfirm?.name}」及其所有账号和托管渠道？此操作不可撤销。`
                : deleteConfirm?.type === "archive-site"
                  ? `确认归档站点「${deleteConfirm?.name}」？归档后将从主列表移除，托管渠道会被下线，账号和密钥会保留；可在『归档站点』中随时恢复。`
                  : `确认删除账号「${deleteConfirm?.name}」及其托管渠道？此操作不可撤销。`}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              className="rounded-xl"
              onClick={() => setDeleteConfirm(null)}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              className="rounded-xl"
              onClick={confirmDelete}
              disabled={
                deleteSite.isPending ||
                deleteSiteAccount.isPending ||
                archiveSite.isPending
              }
            >
              {deleteConfirm?.type === "archive-site"
                ? archiveSite.isPending
                  ? "归档中..."
                  : "确认归档"
                : deleteSite.isPending || deleteSiteAccount.isPending
                  ? "删除中..."
                  : "确认删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
