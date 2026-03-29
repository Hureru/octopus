"use client";

import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ComponentProps,
  type FormEvent,
} from "react";
import { AnimatePresence, motion } from "motion/react";
import {
  type AllAPIHubImportResult,
  Site as SiteRecord,
  SiteAccount,
  SiteCredentialType,
  SitePlatform,
  type CustomHeader,
  useCheckinAllSites,
  useCheckinSiteAccount,
  useCreateSite,
  useCreateSiteAccount,
  useDeleteSite,
  useDeleteSiteAccount,
  useDetectSitePlatform,
  useEnableSite,
  useEnableSiteAccount,
  useImportAllAPIHub,
  useSiteBatchAction,
  useSiteList,
  useSyncAllSites,
  useSyncSiteAccount,
  useUpdateSite,
  useUpdateSiteAccount,
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
import type { SiteFilter as SiteSurfaceFilter } from "@/components/modules/toolbar/view-options-store";
import { cn } from "@/lib/utils";
import { CheckinPanel, type CheckinFilterStatus } from "./CheckinPanel";
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
  ExternalLink,
  FileJson,
  FilterX,
  Link2,
  MoreHorizontal,
  Pencil,
  Pin,
  PinOff,
  Plus,
  RefreshCw,
  Square,
  Trash2,
  TriangleAlert,
  Upload,
  UserRound,
  Waypoints,
  X,
} from "lucide-react";

type SiteFormState = {
  name: string;
  platform: SitePlatform | "";
  base_url: string;
  enabled: boolean;
  proxy: boolean;
  site_proxy: string;
  use_system_proxy: boolean;
  external_checkin_url: string;
  is_pinned: boolean;
  sort_order: number;
  global_weight: number;
  custom_header: CustomHeader[];
};

type SiteAccountFormState = {
  site_id: number;
  name: string;
  credential_type: SiteCredentialType;
  username: string;
  password: string;
  access_token: string;
  api_key: string;
  refresh_token: string;
  token_expires_at: string;
  platform_user_id: string;
  account_proxy: string;
  enabled: boolean;
  auto_sync: boolean;
  auto_checkin: boolean;
  random_checkin: boolean;
  checkin_interval_hours: number;
  checkin_random_window_minutes: number;
};

const AUTO_DETECT_VALUE = "__auto__";

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
  failedAccountCount: number;
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

const SITE_SURFACE_FILTERS: Array<{
  key: SiteSurfaceFilter;
  label: string;
}> = [
  { key: "all", label: "全部站点" },
  { key: "abnormal", label: "异常 / 停用" },
  { key: "enabled", label: "仅启用" },
  { key: "disabled", label: "仅停用" },
  { key: "pinned", label: "仅置顶" },
];

const MENU_BUTTON_CLASS =
  "flex w-full items-center gap-2 rounded-xl px-3 py-2 text-sm text-left transition-colors hover:bg-muted/60";

type SitePendingJump = PendingJump & { target: SiteJumpTarget };

function createEmptySiteForm(): SiteFormState {
  return {
    name: "",
    platform: "",
    base_url: "",
    enabled: true,
    proxy: false,
    site_proxy: "",
    use_system_proxy: false,
    external_checkin_url: "",
    is_pinned: false,
    sort_order: 0,
    global_weight: 1,
    custom_header: [],
  };
}

function createSiteForm(site: SiteRecord): SiteFormState {
  return {
    name: site.name,
    platform: site.platform,
    base_url: site.base_url,
    enabled: site.enabled,
    proxy: site.proxy,
    site_proxy: site.site_proxy ?? "",
    use_system_proxy: site.use_system_proxy,
    external_checkin_url: site.external_checkin_url ?? "",
    is_pinned: site.is_pinned,
    sort_order: site.sort_order,
    global_weight: site.global_weight,
    custom_header: site.custom_header.map((item) => ({ ...item })),
  };
}

function defaultCredentialType(platform: SitePlatform): SiteCredentialType {
  switch (platform) {
    case SitePlatform.Sub2API:
      return SiteCredentialType.AccessToken;
    case SitePlatform.OpenAI:
    case SitePlatform.Claude:
    case SitePlatform.Gemini:
      return SiteCredentialType.APIKey;
    default:
      return SiteCredentialType.UsernamePassword;
  }
}

function credentialOptions(platform: SitePlatform) {
  switch (platform) {
    case SitePlatform.Sub2API:
      return [SiteCredentialType.AccessToken, SiteCredentialType.APIKey];
    case SitePlatform.OpenAI:
    case SitePlatform.Claude:
    case SitePlatform.Gemini:
      return [SiteCredentialType.APIKey, SiteCredentialType.AccessToken];
    default:
      return [
        SiteCredentialType.UsernamePassword,
        SiteCredentialType.AccessToken,
        SiteCredentialType.APIKey,
      ];
  }
}

function createEmptyAccountForm(site: SiteRecord): SiteAccountFormState {
  return {
    site_id: site.id,
    name: "",
    credential_type: defaultCredentialType(site.platform),
    username: "",
    password: "",
    access_token: "",
    api_key: "",
    refresh_token: "",
    token_expires_at: "",
    platform_user_id: "",
    account_proxy: "",
    enabled: true,
    auto_sync: true,
    auto_checkin: true,
    random_checkin: false,
    checkin_interval_hours: 24,
    checkin_random_window_minutes: 120,
  };
}

function createAccountForm(account: SiteAccount): SiteAccountFormState {
  return {
    site_id: account.site_id,
    name: account.name,
    credential_type: account.credential_type,
    username: account.username,
    password: account.password,
    access_token: account.access_token,
    api_key: account.api_key,
    refresh_token: account.refresh_token ?? "",
    token_expires_at:
      account.token_expires_at > 0 ? String(account.token_expires_at) : "",
    platform_user_id: account.platform_user_id
      ? String(account.platform_user_id)
      : "",
    account_proxy: account.account_proxy ?? "",
    enabled: account.enabled,
    auto_sync: account.auto_sync,
    auto_checkin: account.auto_checkin,
    random_checkin: account.random_checkin,
    checkin_interval_hours: account.checkin_interval_hours,
    checkin_random_window_minutes: account.checkin_random_window_minutes,
  };
}

function parseTokenExpiresAtInput(value: string) {
  const trimmed = value.trim();
  if (!trimmed) {
    return 0;
  }

  if (/^\d+$/.test(trimmed)) {
    const parsed = Number(trimmed);
    if (!Number.isFinite(parsed) || parsed <= 0) {
      throw new Error("token_expires_at 必须是正整数时间戳");
    }
    return parsed < 1_000_000_000_000 ? Math.trunc(parsed * 1000) : Math.trunc(parsed);
  }

  const parsed = Date.parse(trimmed);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    throw new Error("token_expires_at 必须是时间戳或可解析时间");
  }
  return Math.trunc(parsed);
}

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

function trimHeaders(items: CustomHeader[]) {
  return items
    .map((item) => ({
      header_key: item.header_key.trim(),
      header_value: item.header_value.trim(),
    }))
    .filter((item) => item.header_key || item.header_value);
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

function sitePlatformSupportsCheckin(platform: SitePlatform) {
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

function accountHasCheckinEnabled(
  account: SiteAccount,
  platform: SitePlatform,
) {
  return sitePlatformSupportsCheckin(platform) && account.auto_checkin;
}

function accountHasSyncFailure(account: SiteAccount) {
  return normalizedStatus(account.last_sync_status) === "failed";
}

function accountHasCheckinFailure(
  account: SiteAccount,
  platform: SitePlatform,
) {
  return (
    accountHasCheckinEnabled(account, platform) &&
    normalizedStatus(account.last_checkin_status) === "failed"
  );
}

function accountHasHealthFailure(
  account: SiteAccount,
  platform: SitePlatform,
) {
  return (
    accountHasSyncFailure(account) || accountHasCheckinFailure(account, platform)
  );
}

function accountMatchesCheckinFilter(
  account: SiteAccount,
  platform: SitePlatform,
  filterStatus: CheckinFilterStatus,
) {
  if (!accountHasCheckinEnabled(account, platform)) {
    return false;
  }
  if (filterStatus === "all") {
    return true;
  }
  return normalizedStatus(account.last_checkin_status) === filterStatus;
}

function statusDotClass(status: string) {
  switch (status) {
    case "success":
      return "bg-emerald-500";
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
      return "border-l-destructive/70 bg-destructive/5";
    case "muted":
      return "border-l-muted-foreground/40 bg-muted/10";
    case "warning":
      return "border-l-amber-500/60 bg-amber-500/5";
    case "default":
    default:
      return "border-l-transparent";
  }
}

function buildSiteSummary(site: SiteRecord): SiteSummary {
  let keyCount = 0;
  let modelCount = 0;
  let groupCount = 0;
  let balance = 0;
  let failedAccountCount = 0;
  let disabledAccountCount = 0;
  let enabledAccountCount = 0;

  for (const account of site.accounts) {
    keyCount += account.tokens.length;
    modelCount += account.models.length;
    groupCount += account.user_groups.length;
    balance += account.balance;

    if (account.enabled) enabledAccountCount += 1;
    else disabledAccountCount += 1;

    if (accountHasHealthFailure(account, site.platform)) {
      failedAccountCount += 1;
    }
  }

  if (failedAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      failedAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: `${failedAccountCount} 异常`,
      healthTone: "danger",
    };
  }

  if (!site.enabled) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      failedAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: "站点停用",
      healthTone: "muted",
    };
  }

  if (disabledAccountCount > 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      failedAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: `${disabledAccountCount} 已停用`,
      healthTone: "muted",
    };
  }

  if (site.accounts.length === 0) {
    return {
      accountCount: site.accounts.length,
      keyCount,
      modelCount,
      groupCount,
      balance,
      failedAccountCount,
      disabledAccountCount,
      enabledAccountCount,
      healthLabel: "待配置",
      healthTone: "warning",
    };
  }

  const allIdle = site.accounts.every(
    (account) =>
      normalizedStatus(account.last_sync_status) === "idle" &&
      (!accountHasCheckinEnabled(account, site.platform) ||
        normalizedStatus(account.last_checkin_status) === "idle"),
  );

  return {
    accountCount: site.accounts.length,
    keyCount,
    modelCount,
    groupCount,
    balance,
    failedAccountCount,
    disabledAccountCount,
    enabledAccountCount,
    healthLabel: allIdle ? "未执行" : "正常",
    healthTone: allIdle ? "warning" : "default",
  };
}

function matchesSiteFilter(
  site: SiteRecord,
  summary: SiteSummary,
  filter: SiteSurfaceFilter,
) {
  switch (filter) {
    case "abnormal":
      return (
        summary.failedAccountCount > 0 ||
        !site.enabled ||
        summary.disabledAccountCount > 0
      );
    case "enabled":
      return site.enabled;
    case "disabled":
      return !site.enabled || summary.disabledAccountCount > 0;
    case "pinned":
      return site.is_pinned;
    case "all":
    default:
      return true;
  }
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

  const summary = text.join(" · ");

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className="flex items-start gap-2 text-xs text-muted-foreground">
          <span
            className={cn(
              "mt-1 size-2 shrink-0 rounded-full",
              statusDotClass(status),
            )}
          />
          <span className="min-w-0 truncate">{summary}</span>
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
  const { data: sites, isLoading, error } = useSiteList();
  const createSite = useCreateSite();
  const updateSite = useUpdateSite();
  const enableSite = useEnableSite();
  const deleteSite = useDeleteSite();
  const createSiteAccount = useCreateSiteAccount();
  const updateSiteAccount = useUpdateSiteAccount();
  const enableSiteAccount = useEnableSiteAccount();
  const deleteSiteAccount = useDeleteSiteAccount();
  const syncSiteAccount = useSyncSiteAccount();
  const checkinSiteAccount = useCheckinSiteAccount();
  const syncAllSites = useSyncAllSites();
  const checkinAllSites = useCheckinAllSites();
  const importAllAPIHub = useImportAllAPIHub();
  const detectPlatform = useDetectSitePlatform();
  const batchAction = useSiteBatchAction();

  const [siteDialogOpen, setSiteDialogOpen] = useState(false);
  const [importDialogOpen, setImportDialogOpen] = useState(false);
  const [importPayloadText, setImportPayloadText] = useState("");
  const [importFile, setImportFile] = useState<File | null>(null);
  const [lastImportResult, setLastImportResult] =
    useState<AllAPIHubImportResult | null>(null);
  const [editingSite, setEditingSite] = useState<SiteRecord | null>(null);
  const [siteForm, setSiteForm] = useState<SiteFormState>(
    createEmptySiteForm(),
  );

  const [accountDialogOpen, setAccountDialogOpen] = useState(false);
  const [accountSite, setAccountSite] = useState<SiteRecord | null>(null);
  const [editingAccount, setEditingAccount] = useState<SiteAccount | null>(
    null,
  );
  const [accountForm, setAccountForm] = useState<SiteAccountFormState | null>(
    null,
  );

  // Batch selection
  const [selectedSiteIds, setSelectedSiteIds] = useState<number[]>([]);

  // Delete confirmation
  const [deleteConfirm, setDeleteConfirm] = useState<{
    type: "site" | "account";
    id: number;
    name: string;
  } | null>(null);
  const [checkinFilterStatus, setCheckinFilterStatus] =
    useState<CheckinFilterStatus>("all");
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
  const siteSurfaceFilter = useToolbarViewOptionsStore(
    (state) => state.siteFilter,
  );
  const setSiteSurfaceFilter = useToolbarViewOptionsStore(
    (state) => state.setSiteFilter,
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
    let siteCount = 0;
    let accountCount = 0;
    let tokenCount = 0;
    let modelCount = 0;

    for (const site of sites ?? []) {
      siteCount += 1;
      accountCount += site.accounts.length;
      for (const account of site.accounts) {
        tokenCount += account.tokens.length;
        modelCount += account.models.length;
      }
    }

    return {
      siteCount,
      accountCount,
      tokenCount,
      modelCount,
    };
  }, [sites]);

  const normalizedQuery = useMemo(
    () => normalizeSearchTerm(searchTerm),
    [searchTerm],
  );

  const visibleSites = useMemo<VisibleSite[]>(() => {
    const hasSearch = normalizedQuery.length > 0;

    return (sites ?? []).flatMap((site) => {
      const summary = buildSiteSummary(site);
      const isForcedTarget = forcedSiteId === site.id;

      if (!isForcedTarget && !matchesSiteFilter(site, summary, siteSurfaceFilter)) {
        return [];
      }

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
      let forceExpanded = checkinFilterStatus !== "all" || isForcedTarget;

      if (checkinFilterStatus !== "all" && !isForcedTarget) {
        visibleAccounts = visibleAccounts.filter((account) =>
          accountMatchesCheckinFilter(
            account,
            site.platform,
            checkinFilterStatus,
          ),
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
          : checkinFilterStatus !== "all"
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
  }, [sites, normalizedQuery, siteSurfaceFilter, checkinFilterStatus, forcedSiteId]);

  const hasActiveFilters =
    normalizedQuery.length > 0 ||
    siteSurfaceFilter !== "all" ||
    checkinFilterStatus !== "all";
  const visibleAccountCount = visibleSites.reduce(
    (sum, item) => sum + item.visibleAccounts.length,
    0,
  );

  const currentPlatform = accountSite?.platform ?? SitePlatform.NewAPI;
  const currentCredentialOptions = credentialOptions(currentPlatform);

  function openCreateSiteDialog() {
    setEditingSite(null);
    setSiteForm(createEmptySiteForm());
    setSiteDialogOpen(true);
  }

  function openEditSiteDialog(site: SiteRecord) {
    setEditingSite(site);
    setSiteForm(createSiteForm(site));
    setSiteDialogOpen(true);
  }

  function closeSiteDialog(open: boolean) {
    setSiteDialogOpen(open);
    if (!open) {
      setEditingSite(null);
      setSiteForm(createEmptySiteForm());
    }
  }

  function openCreateAccountDialog(site: SiteRecord) {
    setAccountSite(site);
    setEditingAccount(null);
    setAccountForm(createEmptyAccountForm(site));
    setAccountDialogOpen(true);
  }

  function openEditAccountDialog(site: SiteRecord, account: SiteAccount) {
    setAccountSite(site);
    setEditingAccount(account);
    setAccountForm(createAccountForm(account));
    setAccountDialogOpen(true);
  }

  function closeAccountDialog(open: boolean) {
    setAccountDialogOpen(open);
    if (!open) {
      setAccountSite(null);
      setEditingAccount(null);
      setAccountForm(null);
    }
  }

  async function submitSiteForm(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    if (!siteForm.name.trim()) {
      toast.error("请输入站点名称");
      return;
    }
    if (!siteForm.base_url.trim()) {
      toast.error("请输入站点地址");
      return;
    }

    let platform = siteForm.platform;
    if (!platform && !editingSite) {
      try {
        const detected = await detectPlatform.mutateAsync(
          siteForm.base_url.trim(),
        );
        platform = detected.platform as SitePlatform;
        toast.success(
          `自动检测到平台：${PLATFORM_LABELS[platform] ?? platform}`,
        );
      } catch {
        toast.error("无法自动检测平台类型，请手动选择");
        return;
      }
    }
    if (!platform) {
      toast.error("请选择平台类型");
      return;
    }

    const customHeader = trimHeaders(siteForm.custom_header);
    const invalidHeader = customHeader.find(
      (item) => !item.header_key || !item.header_value,
    );
    if (invalidHeader) {
      toast.error("自定义 Header 的键和值都不能为空");
      return;
    }

    const payload = {
      name: siteForm.name.trim(),
      platform: platform as SitePlatform,
      base_url: siteForm.base_url.trim(),
      enabled: siteForm.enabled,
      proxy: siteForm.proxy,
      site_proxy: siteForm.site_proxy.trim(),
      use_system_proxy: siteForm.use_system_proxy,
      external_checkin_url: siteForm.external_checkin_url.trim() || null,
      is_pinned: siteForm.is_pinned,
      sort_order: siteForm.sort_order,
      global_weight: siteForm.global_weight,
      custom_header: customHeader,
    };

    try {
      if (editingSite) {
        await updateSite.mutateAsync({ id: editingSite.id, ...payload });
        toast.success("站点已更新");
      } else {
        await createSite.mutateAsync(payload);
        toast.success("站点已创建");
      }
      closeSiteDialog(false);
    } catch (submitError) {
      toast.error(getErrorMessage(submitError));
    }
  }
  async function submitAccountForm(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    if (!accountSite || !accountForm) {
      toast.error("站点上下文不存在");
      return;
    }
    if (!accountForm.name.trim()) {
      toast.error("请输入账号名称");
      return;
    }

    if (accountForm.credential_type === SiteCredentialType.UsernamePassword) {
      if (!accountForm.username.trim() || !accountForm.password.trim()) {
        toast.error("用户名和密码不能为空");
        return;
      }
    }
    if (
      accountForm.credential_type === SiteCredentialType.AccessToken &&
      !accountForm.access_token.trim()
    ) {
      toast.error("请输入 Access Token");
      return;
    }
    if (
      accountForm.credential_type === SiteCredentialType.APIKey &&
      !accountForm.api_key.trim()
    ) {
      toast.error("请输入 API Key");
      return;
    }
    if (accountForm.auto_checkin && accountForm.random_checkin) {
      if (
        !Number.isFinite(accountForm.checkin_interval_hours) ||
        accountForm.checkin_interval_hours < 1 ||
        accountForm.checkin_interval_hours > 720
      ) {
        toast.error("最小签到间隔必须在 1 到 720 小时之间");
        return;
      }
      if (
        !Number.isFinite(accountForm.checkin_random_window_minutes) ||
        accountForm.checkin_random_window_minutes < 0 ||
        accountForm.checkin_random_window_minutes > 1440
      ) {
        toast.error("随机延迟窗口必须在 0 到 1440 分钟之间");
        return;
      }
    }

    const parsedPlatformUserID = accountForm.platform_user_id.trim()
      ? Number(accountForm.platform_user_id.trim())
      : null;
    if (
      parsedPlatformUserID !== null &&
      (!Number.isInteger(parsedPlatformUserID) || parsedPlatformUserID <= 0)
    ) {
      toast.error("Platform User ID 必须是大于 0 的整数");
      return;
    }

    let parsedTokenExpiresAt = 0;
    try {
      parsedTokenExpiresAt = parseTokenExpiresAtInput(
        accountForm.token_expires_at,
      );
    } catch (error) {
      toast.error(getErrorMessage(error));
      return;
    }

    const payload = {
      site_id: accountForm.site_id,
      name: accountForm.name.trim(),
      credential_type: accountForm.credential_type,
      username: accountForm.username.trim(),
      password: accountForm.password.trim(),
      access_token: accountForm.access_token.trim(),
      api_key: accountForm.api_key.trim(),
      refresh_token: accountForm.refresh_token.trim(),
      token_expires_at: parsedTokenExpiresAt,
      platform_user_id: parsedPlatformUserID,
      account_proxy: accountForm.account_proxy.trim(),
      enabled: accountForm.enabled,
      auto_sync: accountForm.auto_sync,
      auto_checkin: accountForm.auto_checkin,
      random_checkin: accountForm.random_checkin,
      checkin_interval_hours: Math.max(
        1,
        Math.trunc(accountForm.checkin_interval_hours || 24),
      ),
      checkin_random_window_minutes: Math.max(
        0,
        Math.trunc(accountForm.checkin_random_window_minutes || 0),
      ),
    };

    try {
      if (editingAccount) {
        await updateSiteAccount.mutateAsync({
          id: editingAccount.id,
          ...payload,
        });
        toast.success("站点账号已更新");
      } else {
        await createSiteAccount.mutateAsync(payload);
        toast.success("站点账号已创建");
      }
      closeAccountDialog(false);
    } catch (submitError) {
      toast.error(getErrorMessage(submitError));
    }
  }

  async function handleToggleSite(site: SiteRecord) {
    try {
      await enableSite.mutateAsync({ id: site.id, enabled: !site.enabled });
      toast.success(site.enabled ? "站点已停用" : "站点已启用");
    } catch (toggleError) {
      toast.error(getErrorMessage(toggleError));
    }
  }

  async function handleDeleteSite(site: SiteRecord) {
    setDeleteConfirm({ type: "site", id: site.id, name: site.name });
  }

  async function handleToggleAccount(account: SiteAccount) {
    try {
      await enableSiteAccount.mutateAsync({
        id: account.id,
        enabled: !account.enabled,
      });
      toast.success(account.enabled ? "站点账号已停用" : "站点账号已启用");
    } catch (toggleError) {
      toast.error(getErrorMessage(toggleError));
    }
  }

  async function handleDeleteAccount(account: SiteAccount) {
    setDeleteConfirm({ type: "account", id: account.id, name: account.name });
  }

  async function handleSyncAccount(account: SiteAccount) {
    setSyncingAccountIds((current) => new Set(current).add(account.id));
    try {
      const result = await syncSiteAccount.mutateAsync(account.id);
      toast.success(
        `同步完成：${result.group_count} 个分组，${result.token_count} 个 key，${result.model_count} 个模型`,
      );
    } catch (syncError) {
      toast.error(getErrorMessage(syncError));
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
      toast.error(getErrorMessage(checkinError));
    } finally {
      setCheckinAccountIds((current) => {
        const next = new Set(current);
        next.delete(account.id);
        return next;
      });
    }
  }

  async function handleImportAllAPIHub() {
    const hasFile = !!importFile;
    const hasText = !!importPayloadText.trim();
    if (!hasFile && !hasText) {
      toast.error("请选择 JSON 文件或粘贴 All API Hub 导出内容");
      return;
    }

    try {
      const result = await importAllAPIHub.mutateAsync({
        file: importFile,
        text: importPayloadText,
      });
      setLastImportResult(result);
      setImportFile(null);
      setImportPayloadText("");
      toast.success(
        `导入完成：新增 ${result.created_sites} 个站点，新增 ${result.created_accounts} 个账号，更新 ${result.updated_accounts} 个账号`,
      );
    } catch (importError) {
      toast.error(getErrorMessage(importError));
    }
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
      } else {
        await deleteSiteAccount.mutateAsync(deleteConfirm.id);
        toast.success("站点账号已删除");
      }
    } catch (deleteError) {
      toast.error(getErrorMessage(deleteError));
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
      toast.error(getErrorMessage(batchError));
    }
  }

  async function handleTogglePin(site: SiteRecord) {
    try {
      await updateSite.mutateAsync({ id: site.id, is_pinned: !site.is_pinned });
      toast.success(site.is_pinned ? "已取消置顶" : "已置顶");
    } catch (pinError) {
      toast.error(getErrorMessage(pinError));
    }
  }

  function clearFilters() {
    setSearchTerm("site", "");
    setSiteSurfaceFilter("all");
    setCheckinFilterStatus("all");
  }

  function openSiteBaseURL(site: SiteRecord) {
    window.open(site.base_url, "_blank", "noopener,noreferrer");
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
        setSiteForm(createEmptySiteForm());
        setSiteDialogOpen(true);
      },
      openImportDialog: () => setImportDialogOpen(true),
      syncAll: () => {
        syncAllSites.mutate(undefined, {
          onSuccess: () => toast.success("已触发后台全量同步，页面会自动刷新"),
          onError: (error) => toast.error(getErrorMessage(error)),
        });
      },
      checkinAll: () => {
        checkinAllSites.mutate(undefined, {
          onSuccess: () => toast.success("已触发后台全量签到，页面会自动刷新"),
          onError: (error) => toast.error(getErrorMessage(error)),
        });
      },
    });

    return () => {
      resetSiteHandlers();
    };
  }, [setSiteHandlers, resetSiteHandlers, syncAllSites, checkinAllSites]);

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
          "rounded-[28px] border border-border/70 border-l-4 bg-card p-5 shadow-[0_20px_60px_-42px_rgba(15,23,42,0.45)]",
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
              <button
                type="button"
                className="min-w-0 flex-1 text-left"
                onClick={() => toggleSiteExpanded(site.id, forceExpanded)}
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
                  <span className="truncate">{site.base_url}</span>
                </div>

                <div className="mt-3 flex flex-wrap gap-x-4 gap-y-2">
                  <CompactMetric label="账号" value={summary.accountCount} />
                  <CompactMetric label="Key" value={summary.keyCount} />
                  <CompactMetric label="模型" value={summary.modelCount} />
                  <CompactMetric label="余额" value={formatBalance(summary.balance)} />
                </div>

                <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                  <span>
                    {site.proxy
                      ? "站点代理"
                      : site.use_system_proxy
                        ? "系统代理"
                        : "直连"}
                  </span>
                  {site.site_proxy ? (
                    <span className="truncate">代理 {site.site_proxy}</span>
                  ) : null}
                  {site.custom_header.length > 0 ? (
                    <span>{site.custom_header.length} 个 Header</span>
                  ) : null}
                  {site.external_checkin_url ? <span>外部签到</span> : null}
                </div>
              </button>

              <div className="flex items-center gap-1">
                <IconActionButton
                  label="打开站点"
                  onClick={() => openSiteBaseURL(site)}
                >
                  <ExternalLink className="size-4" />
                </IconActionButton>

                <IconActionButton
                  label="查看站点渠道"
                  onClick={() => jumpToSiteChannel(site.id)}
                >
                  <Waypoints className="size-4" />
                </IconActionButton>

                <IconActionButton
                  label="新增账号"
                  onClick={() => openCreateAccountDialog(site)}
                >
                  <Plus className="size-4" />
                </IconActionButton>

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
                    className="w-48 rounded-2xl border border-border/60 bg-card p-2"
                  >
                    <div className="grid gap-1">
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
                        <span>{site.enabled ? "停用站点" : "启用站点"}</span>
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
                          const accountFailed = accountHasHealthFailure(
                            account,
                            site.platform,
                          );
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
                                "rounded-2xl border border-border/60 border-l-4 bg-gradient-to-br from-muted/25 via-background to-background px-4 py-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.04)]",
                                cardToneClass(accountTone),
                                highlightedAccountId === account.id &&
                                  "ring-2 ring-primary/35 ring-offset-2 ring-offset-background",
                              )}
                            >
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
                                    {account.account_proxy ? (
                                      <span className="truncate">
                                        代理 {account.account_proxy}
                                      </span>
                                    ) : null}
                                  </div>

                                  <div className="space-y-1">
                                    <ExecutionSummary
                                      label="同步"
                                      status={normalizedStatus(
                                        account.last_sync_status,
                                      )}
                                      at={account.last_sync_at}
                                      message={
                                        account.last_sync_message || "等待首次同步"
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

                                <div className="flex items-center gap-2">
                                  <IconActionButton
                                    label="查看站点渠道"
                                    onClick={() =>
                                      jumpToSiteChannelAccount(site.id, account.id)
                                    }
                                  >
                                    <Waypoints className="size-4" />
                                  </IconActionButton>

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
          visibleSiteCount={visibleSites.length}
          visibleAccountCount={visibleAccountCount}
          searchTerm={searchTerm.trim()}
          siteFilterLabel={
            siteSurfaceFilter === "all"
              ? null
              : SITE_SURFACE_FILTERS.find(
                  (filter) => filter.key === siteSurfaceFilter,
                )?.label ?? null
          }
          hasActiveFilters={hasActiveFilters}
          onClearFilters={clearFilters}
          filterStatus={checkinFilterStatus}
          onFilterChange={setCheckinFilterStatus}
        />

        {selectedSiteIds.length > 0 ? (
          <section className="rounded-3xl border border-primary/30 bg-primary/5 p-4">
            <div className="flex flex-wrap items-center gap-3">
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
                variant="outline"
                size="sm"
                className="rounded-xl"
                onClick={() => handleBatchAction("enable_system_proxy")}
                disabled={batchAction.isPending}
              >
                批量启用系统代理
              </Button>
              <Button
                variant="outline"
                size="sm"
                className="rounded-xl"
                onClick={() => handleBatchAction("disable_system_proxy")}
                disabled={batchAction.isPending}
              >
                批量禁用系统代理
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
            站点列表加载失败：{getErrorMessage(error)}
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

      <Dialog open={siteDialogOpen} onOpenChange={closeSiteDialog}>
        <DialogContent className="max-w-4xl rounded-3xl">
          <DialogHeader>
            <DialogTitle>{editingSite ? "编辑站点" : "新增站点"}</DialogTitle>
            <DialogDescription>
              配置站点平台、代理和自定义
              Header。站点账号会基于这里的基础信息进行同步。
            </DialogDescription>
          </DialogHeader>

          <form className="space-y-5" onSubmit={submitSiteForm}>
            <div className="grid gap-4 md:grid-cols-2">
              <label className="grid gap-2 text-sm">
                <span className="font-medium">站点名称</span>
                <Input
                  value={siteForm.name}
                  onChange={(event) =>
                    setSiteForm((current) => ({
                      ...current,
                      name: event.target.value,
                    }))
                  }
                  placeholder="例如：主站 OneAPI"
                  className="rounded-xl"
                />
              </label>

              <label className="grid gap-2 text-sm">
                <span className="font-medium">平台类型</span>
                <Select
                  value={siteForm.platform || AUTO_DETECT_VALUE}
                  onValueChange={(value) =>
                    setSiteForm((current) => ({
                      ...current,
                      platform:
                        value === AUTO_DETECT_VALUE
                          ? ""
                          : (value as SitePlatform),
                    }))
                  }
                >
                  <SelectTrigger className="w-full rounded-xl">
                    <SelectValue placeholder="自动检测" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={AUTO_DETECT_VALUE}>自动检测</SelectItem>
                    {Object.entries(PLATFORM_LABELS).map(([value, label]) => (
                      <SelectItem key={value} value={value}>
                        {label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </label>
            </div>

            <label className="grid gap-2 text-sm">
              <span className="font-medium">站点地址</span>
              <Input
                value={siteForm.base_url}
                onChange={(event) =>
                  setSiteForm((current) => ({
                    ...current,
                    base_url: event.target.value,
                  }))
                }
                placeholder="https://example.com"
                className="rounded-xl"
              />
            </label>

            <div className="grid gap-4 md:grid-cols-3">
              <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                <div>
                  <div className="text-sm font-medium">启用站点</div>
                  <div className="text-xs text-muted-foreground">
                    停用后不再投影托管渠道
                  </div>
                </div>
                <Switch
                  checked={siteForm.enabled}
                  onCheckedChange={(checked) =>
                    setSiteForm((current) => ({ ...current, enabled: checked }))
                  }
                />
              </div>

              <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                <div>
                  <div className="text-sm font-medium">站点代理</div>
                  <div className="text-xs text-muted-foreground">
                    请求时走站点级代理
                  </div>
                </div>
                <Switch
                  checked={siteForm.proxy}
                  onCheckedChange={(checked) =>
                    setSiteForm((current) => ({ ...current, proxy: checked }))
                  }
                />
              </div>

              <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                <div>
                  <div className="text-sm font-medium">系统代理</div>
                  <div className="text-xs text-muted-foreground">
                    无专用代理时用全局代理
                  </div>
                </div>
                <Switch
                  checked={siteForm.use_system_proxy}
                  onCheckedChange={(checked) =>
                    setSiteForm((current) => ({
                      ...current,
                      use_system_proxy: checked,
                    }))
                  }
                />
              </div>
            </div>

            <label className="grid gap-2 text-sm">
              <span className="font-medium">站点级代理</span>
              <Input
                value={siteForm.site_proxy}
                onChange={(event) =>
                  setSiteForm((current) => ({
                    ...current,
                    site_proxy: event.target.value,
                  }))
                }
                placeholder="可选：例如 socks5://127.0.0.1:7890"
                className="rounded-xl"
              />
            </label>

            <label className="grid gap-2 text-sm">
              <span className="font-medium">外部签到 URL</span>
              <Input
                value={siteForm.external_checkin_url}
                onChange={(event) =>
                  setSiteForm((current) => ({
                    ...current,
                    external_checkin_url: event.target.value,
                  }))
                }
                placeholder="可选：例如 https://example.com/api/checkin"
                className="rounded-xl"
              />
              <span className="text-xs text-muted-foreground">
                配置后签到将调用此
                URL，而非内置平台逻辑。适用于平台不支持签到但有外部签到接口的场景。
              </span>
            </label>

            <div className="space-y-3 rounded-2xl border border-border/60 bg-muted/10 p-4">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <div className="text-sm font-medium">自定义 Header</div>
                  <div className="text-xs text-muted-foreground">
                    会透传到站点接口请求，可用于附加鉴权或租户信息
                  </div>
                </div>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="rounded-xl"
                  onClick={() =>
                    setSiteForm((current) => ({
                      ...current,
                      custom_header: [
                        ...current.custom_header,
                        { header_key: "", header_value: "" },
                      ],
                    }))
                  }
                >
                  <Plus className="size-4" />
                  添加 Header
                </Button>
              </div>

              {siteForm.custom_header.length === 0 ? (
                <div className="text-sm text-muted-foreground">
                  暂无自定义 Header
                </div>
              ) : null}
              <div className="space-y-3">
                {siteForm.custom_header.map((item, index) => (
                  <div
                    key={`${index}-${item.header_key}`}
                    className="grid gap-3 md:grid-cols-[1fr_1fr_auto]"
                  >
                    <Input
                      value={item.header_key}
                      onChange={(event) =>
                        setSiteForm((current) => ({
                          ...current,
                          custom_header: current.custom_header.map(
                            (header, headerIndex) =>
                              headerIndex === index
                                ? { ...header, header_key: event.target.value }
                                : header,
                          ),
                        }))
                      }
                      placeholder="Header Key"
                      className="rounded-xl"
                    />
                    <Input
                      value={item.header_value}
                      onChange={(event) =>
                        setSiteForm((current) => ({
                          ...current,
                          custom_header: current.custom_header.map(
                            (header, headerIndex) =>
                              headerIndex === index
                                ? {
                                    ...header,
                                    header_value: event.target.value,
                                  }
                                : header,
                          ),
                        }))
                      }
                      placeholder="Header Value"
                      className="rounded-xl"
                    />
                    <Button
                      type="button"
                      variant="outline"
                      size="icon"
                      className="rounded-xl"
                      onClick={() =>
                        setSiteForm((current) => ({
                          ...current,
                          custom_header: current.custom_header.filter(
                            (_, headerIndex) => headerIndex !== index,
                          ),
                        }))
                      }
                    >
                      <X className="size-4" />
                    </Button>
                  </div>
                ))}
              </div>
            </div>

            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                className="rounded-xl"
                onClick={() => closeSiteDialog(false)}
              >
                取消
              </Button>
              <Button
                type="submit"
                className="rounded-xl"
                disabled={createSite.isPending || updateSite.isPending}
              >
                {createSite.isPending || updateSite.isPending
                  ? "保存中..."
                  : "保存"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={accountDialogOpen} onOpenChange={closeAccountDialog}>
        <DialogContent className="max-w-3xl rounded-3xl">
          <DialogHeader>
            <DialogTitle>
              {editingAccount ? "编辑站点账号" : "新增站点账号"}
            </DialogTitle>
            <DialogDescription>
              {accountSite
                ? `账号会挂载到站点「${accountSite.name}」下，并按同步结果自动投影 channel。`
                : "配置站点账号后，可自动同步分组、模型和托管渠道。"}
            </DialogDescription>
          </DialogHeader>

          {accountForm ? (
            <form className="space-y-5" onSubmit={submitAccountForm}>
              <div className="grid gap-4 md:grid-cols-2">
                <label className="grid gap-2 text-sm">
                  <span className="font-medium">账号名称</span>
                  <Input
                    value={accountForm.name}
                    onChange={(event) =>
                      setAccountForm((current) =>
                        current
                          ? { ...current, name: event.target.value }
                          : current,
                      )
                    }
                    placeholder="例如：主账号"
                    className="rounded-xl"
                  />
                </label>

                <label className="grid gap-2 text-sm">
                  <span className="font-medium">凭据类型</span>
                  <Select
                    value={accountForm.credential_type}
                    onValueChange={(value) =>
                      setAccountForm((current) =>
                        current
                          ? {
                              ...current,
                              credential_type: value as SiteCredentialType,
                            }
                          : current,
                      )
                    }
                  >
                    <SelectTrigger className="w-full rounded-xl">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {currentCredentialOptions.map((value) => (
                        <SelectItem key={value} value={value}>
                          {CREDENTIAL_LABELS[value]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </label>
              </div>

              {accountForm.credential_type ===
              SiteCredentialType.UsernamePassword ? (
                <div className="grid gap-4 md:grid-cols-2">
                  <label className="grid gap-2 text-sm">
                    <span className="font-medium">用户名</span>
                    <Input
                      value={accountForm.username}
                      onChange={(event) =>
                        setAccountForm((current) =>
                          current
                            ? { ...current, username: event.target.value }
                            : current,
                        )
                      }
                      placeholder="请输入用户名"
                      className="rounded-xl"
                    />
                  </label>

                  <label className="grid gap-2 text-sm">
                    <span className="font-medium">密码</span>
                    <Input
                      type="password"
                      value={accountForm.password}
                      onChange={(event) =>
                        setAccountForm((current) =>
                          current
                            ? { ...current, password: event.target.value }
                            : current,
                        )
                      }
                      placeholder="请输入密码"
                      className="rounded-xl"
                    />
                  </label>
                </div>
              ) : null}

              {accountForm.credential_type ===
              SiteCredentialType.AccessToken ? (
                <div className="grid gap-4">
                  <label className="grid gap-2 text-sm">
                    <span className="font-medium">Access Token</span>
                    <Input
                      value={accountForm.access_token}
                      onChange={(event) =>
                        setAccountForm((current) =>
                          current
                            ? { ...current, access_token: event.target.value }
                            : current,
                        )
                      }
                      placeholder="请输入 Access Token"
                      className="rounded-xl"
                    />
                  </label>

                  {currentPlatform === SitePlatform.Sub2API ? (
                    <div className="grid gap-2">
                      <div className="grid gap-4 md:grid-cols-2">
                        <label className="grid gap-2 text-sm">
                          <span className="font-medium">Refresh Token</span>
                          <Input
                            value={accountForm.refresh_token}
                            onChange={(event) =>
                              setAccountForm((current) =>
                                current
                                  ? {
                                      ...current,
                                      refresh_token: event.target.value,
                                    }
                                  : current,
                              )
                            }
                            placeholder="可选：请输入 refresh_token"
                            className="rounded-xl"
                          />
                        </label>

                        <label className="grid gap-2 text-sm">
                          <span className="font-medium">token_expires_at</span>
                          <Input
                            value={accountForm.token_expires_at}
                            onChange={(event) =>
                              setAccountForm((current) =>
                                current
                                  ? {
                                      ...current,
                                      token_expires_at: event.target.value,
                                    }
                                  : current,
                              )
                            }
                            placeholder="可选：F12 中的时间戳或时间字符串"
                            className="rounded-xl"
                          />
                        </label>
                      </div>
                      <span className="text-xs text-muted-foreground">
                        Sub2API 推荐同时填写 F12 里的 <code>refresh_token</code>{" "}
                        与 <code>token_expires_at</code>，会在快过期或 401
                        时自动续期。
                      </span>
                    </div>
                  ) : null}
                </div>
              ) : null}

              {accountForm.credential_type === SiteCredentialType.APIKey ? (
                <label className="grid gap-2 text-sm">
                  <span className="font-medium">API Key</span>
                  <Input
                    value={accountForm.api_key}
                    onChange={(event) =>
                      setAccountForm((current) =>
                        current
                          ? { ...current, api_key: event.target.value }
                          : current,
                      )
                    }
                    placeholder="请输入 API Key"
                    className="rounded-xl"
                  />
                </label>
              ) : null}

              {currentPlatform === SitePlatform.NewAPI ? (
                <label className="grid gap-2 text-sm">
                  <span className="font-medium">Platform User ID</span>
                  <Input
                    value={accountForm.platform_user_id}
                    onChange={(event) =>
                      setAccountForm((current) =>
                        current
                          ? { ...current, platform_user_id: event.target.value }
                          : current,
                      )
                    }
                    placeholder="可选：例如 11494"
                    className="rounded-xl"
                  />
                  <span className="text-xs text-muted-foreground">
                    部分 New API 站点同步 token、分组和签到时要求额外提供用户
                    ID。All API Hub 导入会自动填充该值。
                  </span>
                </label>
              ) : null}

              <label className="grid gap-2 text-sm">
                <span className="font-medium">账号级代理</span>
                <Input
                  value={accountForm.account_proxy}
                  onChange={(event) =>
                    setAccountForm((current) =>
                      current
                        ? { ...current, account_proxy: event.target.value }
                        : current,
                    )
                  }
                  placeholder="可选：例如 socks5://127.0.0.1:7890"
                  className="rounded-xl"
                />
                <span className="text-xs text-muted-foreground">
                  用于该账号的同步、签到和模型拉取；自动投影的 channel
                  默认也会跟随这里的代理。
                </span>
              </label>

              <div className="grid gap-4 md:grid-cols-2">
                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <UserRound className="size-4 text-muted-foreground" />
                      <span className="text-sm font-medium">启用账号</span>
                    </div>
                    <div className="text-xs text-muted-foreground">
                      停用后不参与同步和签到
                    </div>
                  </div>
                  <Switch
                    checked={accountForm.enabled}
                    onCheckedChange={(checked) =>
                      setAccountForm((current) =>
                        current ? { ...current, enabled: checked } : current,
                      )
                    }
                  />
                </div>

                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <RefreshCw className="size-4 text-muted-foreground" />
                      <span className="text-sm font-medium">自动同步</span>
                    </div>
                    <div className="text-xs text-muted-foreground">
                      定时拉取分组、模型和 key
                    </div>
                  </div>
                  <Switch
                    checked={accountForm.auto_sync}
                    onCheckedChange={(checked) =>
                      setAccountForm((current) =>
                        current ? { ...current, auto_sync: checked } : current,
                      )
                    }
                  />
                </div>

                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <CalendarCheck2 className="size-4 text-muted-foreground" />
                      <span className="text-sm font-medium">自动签到</span>
                    </div>
                    <div className="text-xs text-muted-foreground">
                      定时执行平台签到
                    </div>
                  </div>
                  <Switch
                    checked={accountForm.auto_checkin}
                    onCheckedChange={(checked) =>
                      setAccountForm((current) =>
                        current
                          ? { ...current, auto_checkin: checked }
                          : current,
                      )
                    }
                  />
                </div>

                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                  <div>
                    <div className="flex items-center gap-2">
                      <CalendarCheck2 className="size-4 text-muted-foreground" />
                      <span className="text-sm font-medium">随机签到</span>
                    </div>
                    <div className="text-xs text-muted-foreground">
                      在间隔基础上添加随机延迟
                    </div>
                  </div>
                  <Switch
                    checked={accountForm.random_checkin}
                    onCheckedChange={(checked) =>
                      setAccountForm((current) =>
                        current
                          ? { ...current, random_checkin: checked }
                          : current,
                      )
                    }
                  />
                </div>
              </div>

              {accountForm.auto_checkin && accountForm.random_checkin ? (
                <div className="grid gap-4 md:grid-cols-2">
                  <label className="grid gap-2 text-sm">
                    <span className="font-medium">最小签到间隔（小时）</span>
                    <Input
                      type="number"
                      min={1}
                      max={720}
                      value={accountForm.checkin_interval_hours}
                      onChange={(event) =>
                        setAccountForm((current) =>
                          current
                            ? {
                                ...current,
                                checkin_interval_hours: Number(
                                  event.target.value,
                                ),
                              }
                            : current,
                        )
                      }
                      placeholder="24"
                      className="rounded-xl"
                    />
                  </label>

                  <label className="grid gap-2 text-sm">
                    <span className="font-medium">随机延迟窗口（分钟）</span>
                    <Input
                      type="number"
                      min={0}
                      max={1440}
                      value={accountForm.checkin_random_window_minutes}
                      onChange={(event) =>
                        setAccountForm((current) =>
                          current
                            ? {
                                ...current,
                                checkin_random_window_minutes: Number(
                                  event.target.value,
                                ),
                              }
                            : current,
                        )
                      }
                      placeholder="120"
                      className="rounded-xl"
                    />
                  </label>
                </div>
              ) : null}

              <div className="grid gap-3 rounded-2xl border border-border/60 bg-muted/10 p-4 text-sm text-muted-foreground">
                <div className="flex items-center gap-2">
                  <Waypoints className="size-4" />
                  <span>投影规则说明</span>
                </div>
                <p>
                  同步后会以 <code>site_user_group</code> 为主维度生成托管
                  channel；无分组时使用 <code>default</code>。同一分组下的多个
                  key 会聚合到同一个 channel；多端点兼容站点会按模型已归属的
                  请求端点格式继续拆成独立托管 channel。
                </p>
                {accountForm.auto_checkin && accountForm.random_checkin ? (
                  <p>
                    随机签到会基于“上次成功签到时间 + 最小间隔 + 0
                    到随机延迟窗口”的规则生成下次执行时间，适合需要接近 24
                    小时间隔的站点。
                  </p>
                ) : null}
              </div>

              <DialogFooter>
                <Button
                  type="button"
                  variant="outline"
                  className="rounded-xl"
                  onClick={() => closeAccountDialog(false)}
                >
                  取消
                </Button>
                <Button
                  type="submit"
                  className="rounded-xl"
                  disabled={
                    createSiteAccount.isPending || updateSiteAccount.isPending
                  }
                >
                  {createSiteAccount.isPending || updateSiteAccount.isPending
                    ? "保存中..."
                    : "保存"}
                </Button>
              </DialogFooter>
            </form>
          ) : null}
        </DialogContent>
      </Dialog>

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
              导入 All API Hub 账号
            </DialogTitle>
            <DialogDescription>
              支持直接上传 All API Hub 导出的 JSON
              文件，或粘贴完整导出内容。导入后会按平台和站点地址自动创建或复用站点，并为可同步账号触发后台同步。
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-5">
            <div className="space-y-3 rounded-2xl border border-border/60 bg-muted/10 p-4">
              <div className="text-sm font-medium">上传 JSON 文件</div>
              <Input
                type="file"
                accept=".json,application/json"
                onChange={(event) => {
                  setImportFile(event.target.files?.[0] ?? null);
                  setLastImportResult(null);
                }}
                className="rounded-xl"
              />
              <div className="text-xs text-muted-foreground">
                {importFile
                  ? `已选择：${importFile.name}`
                  : "支持 All API Hub 导出的 .json 文件"}
              </div>
              {importFile ? (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  className="rounded-xl"
                  onClick={() => setImportFile(null)}
                >
                  <X className="size-4" />
                  清除文件
                </Button>
              ) : null}
            </div>

            <label className="grid gap-2 text-sm">
              <span className="font-medium">或粘贴导出 JSON</span>
              <textarea
                value={importPayloadText}
                onChange={(event) => {
                  setImportPayloadText(event.target.value);
                  setLastImportResult(null);
                }}
                placeholder='粘贴类似 {"accounts":{"accounts":[...]}} 的完整导出内容'
                className="min-h-40 rounded-2xl border border-input bg-background px-4 py-3 font-mono text-xs outline-none transition focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/20"
              />
              <span className="text-xs text-muted-foreground">
                导入会保留已存在站点的本地配置；同一分组下的多个 key
                后续仍会聚合到同一个托管 channel。
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
                  <SiteMetric
                    label="后台同步"
                    value={lastImportResult.scheduled_sync_accounts}
                  />
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
              onClick={handleImportAllAPIHub}
              disabled={importAllAPIHub.isPending}
              className="rounded-xl"
            >
              <Upload
                className={cn(
                  "size-4",
                  importAllAPIHub.isPending ? "animate-pulse" : "",
                )}
              />
              {importAllAPIHub.isPending ? "导入中..." : "开始导入"}
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
            <DialogTitle>确认删除</DialogTitle>
            <DialogDescription>
              {deleteConfirm?.type === "site"
                ? `确认删除站点「${deleteConfirm?.name}」及其所有账号和托管渠道？此操作不可撤销。`
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
              disabled={deleteSite.isPending || deleteSiteAccount.isPending}
            >
              {deleteSite.isPending || deleteSiteAccount.isPending
                ? "删除中..."
                : "确认删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
