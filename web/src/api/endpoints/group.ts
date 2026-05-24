import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import { logger } from '@/lib/logger';
import { AutoGroupType } from './channel';

/**
 * 分组项信息
 */
export interface GroupItem {
    id?: number;
    group_id?: number;
    channel_id: number;
    model_name: string;
    priority: number;
    weight: number;
}

/**
 * 分组模式
 */
export enum GroupMode {
    RoundRobin = 1,
    Random = 2,
    Failover = 3,
    Weighted = 4,
}

/**
 * 分组信息
 */
export interface Group {
    id?: number;
    name: string;
    mode: GroupMode;
    match_regex: string;
    first_token_time_out?: number;
    session_keep_time?: number;
    retry_enabled?: boolean;
    max_retries?: number;
    pinned?: boolean;
    pinned_at?: string | null;
    active_preset_id?: number | null;
    items?: GroupItem[];
}

/**
 * 预设中的渠道-模型条目（JSON 快照内容）
 */
export interface GroupPresetItem {
    channel_id: number;
    model_name: string;
    priority: number;
    weight: number;
}

/**
 * 分组预设：命名快照，包含 Mode/超时/重试/regex + items
 */
export interface GroupPreset {
    id: number;
    group_id: number;
    name: string;
    mode: GroupMode;
    match_regex: string;
    first_token_time_out: number;
    session_keep_time: number;
    retry_enabled: boolean;
    max_retries: number;
    items: GroupPresetItem[];
    created_at: string;
    updated_at: string;
}

/**
 * 预设直接编辑请求（仅非活动预设可调）
 * Items 为整体替换语义
 */
export interface GroupPresetUpdateRequest {
    name?: string;
    mode?: GroupMode;
    match_regex?: string;
    first_token_time_out?: number;
    session_keep_time?: number;
    retry_enabled?: boolean;
    max_retries?: number;
    items?: GroupPresetItem[];
}

/**
 * 新增 item 请求
 */
export interface GroupItemAddRequest {
    channel_id: number;
    model_name: string;
    priority: number;
    weight: number;
}

/**
 * 更新 item 请求 (仅 priority)
 */
export interface GroupItemUpdateRequest {
    id: number;
    priority: number;
    weight: number;
}

/**
 * 分组更新请求 - 仅包含变更的数据
 */
export interface GroupUpdateRequest {
    id: number;
    name?: string;                        // 仅在名称变更时发送
    mode?: GroupMode;                     // 仅在模式变更时发送
    match_regex?: string;                 // 仅在匹配正则变更时发送
    first_token_time_out?: number;        // 仅在超时变更时发送
    session_keep_time?: number;           // 仅在会话保持时间变更时发送
    retry_enabled?: boolean;              // 仅在同通道重试开关变更时发送
    max_retries?: number;                 // 仅在最大重试次数变更时发送
    items_to_add?: GroupItemAddRequest[];    // 新增的 items
    items_to_update?: GroupItemUpdateRequest[]; // 更新的 items (priority 变更)
    items_to_delete?: number[];              // 删除的 item IDs
}

export interface GroupAutoGroupSource {
    channel_id: number;
    channel_name: string;
    enabled: boolean;
    managed: boolean;
    auto_group: AutoGroupType;
    effective_auto_group: AutoGroupType;
    global_override: boolean;
    model_count: number;
    models: string[];
    site_id?: number | null;
    site_name?: string;
    site_account_id?: number | null;
    site_account_name?: string;
    site_group_key?: string;
    site_group_name?: string;
    endpoint_type?: string;
}

export interface GroupAutoGroupConfig {
    projected_global_auto_group: AutoGroupType;
    sources: GroupAutoGroupSource[];
}

export interface GroupAutoGroupSourceUpdateRequest {
    channel_id: number;
    auto_group: AutoGroupType;
}

export interface GroupAutoGroupConfigUpdateRequest {
    projected_global_auto_group?: AutoGroupType;
    items?: GroupAutoGroupSourceUpdateRequest[];
    run_now?: boolean;
}

export interface GroupAutoGroupRunRequest {
    channel_ids?: number[];
}

/**
 * 获取分组列表 Hook
 * 
 * @example
 * const { data: groups, isLoading, error } = useGroupList();
 * 
 * if (isLoading) return <Loading />;
 * if (error) return <Error message={error.message} />;
 * 
 * groups?.forEach(group => console.log(group.name, group.items));
 */
export function useGroupList() {
    return useQuery({
        queryKey: ['groups', 'list'],
        queryFn: async () => {
            return apiClient.get<Group[]>('/api/v1/group/list');
        },
        refetchInterval: 30000,
    });
}

/**
 * 创建分组 Hook
 * 
 * @example
 * const createGroup = useCreateGroup();
 * 
 * createGroup.mutate({
 *   name: 'my-group',
 *   items: [
 *     { channel_id: 1, model_name: 'gpt-4', priority: 1 },
 *   ],
 * });
 */
export function useCreateGroup() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: Group) => {
            return apiClient.post<Group>('/api/v1/group/create', data);
        },
        onSuccess: (data) => {
            logger.log('分组创建成功:', data);
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
        },
        onError: (error) => {
            logger.error('分组创建失败:', error);
        },
    });
}

/**
 * 更新分组 Hook - 仅发送变更的数据
 * 
 * @example
 * const updateGroup = useUpdateGroup();
 * 
 * updateGroup.mutate({
 *   id: 1,
 *   name: 'updated-group',  // 可选，仅在名称变更时发送
 *   items_to_add: [{ channel_id: 1, model_name: 'gpt-4', priority: 1 }],
 *   items_to_update: [{ id: 1, priority: 2 }],
 *   items_to_delete: [2, 3],
 * });
 */
export function useUpdateGroup() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: GroupUpdateRequest) => {
            return apiClient.post<Group>('/api/v1/group/update', data);
        },
        onSuccess: (data) => {
            logger.log('分组更新成功:', data);
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
        },
        onError: (error) => {
            logger.error('分组更新失败:', error);
        },
    });
}

/**
 * 删除分组 Hook
 * 
 * @example
 * const deleteGroup = useDeleteGroup();
 * 
 * deleteGroup.mutate(1); // 删除 ID 为 1 的分组
 */
export function useDeleteGroup() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (id: number) => {
            return apiClient.delete<null>(`/api/v1/group/delete/${id}`);
        },
        onSuccess: () => {
            logger.log('分组删除成功');
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
        },
        onError: (error) => {
            logger.error('分组删除失败:', error);
        },
    });
}

export function useGroupAutoGroupConfig() {
    return useQuery({
        queryKey: ['groups', 'auto-group', 'config'],
        queryFn: async () => apiClient.get<GroupAutoGroupConfig>('/api/v1/group/auto-group/config'),
        refetchInterval: 30000,
    });
}

function invalidateAutoGroupRelated(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['groups', 'auto-group', 'config'] });
    queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['channels', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['models', 'channel'] });
    queryClient.invalidateQueries({ queryKey: ['site-channel', 'list'] });
    queryClient.invalidateQueries({ queryKey: ['settings', 'list'] });
}

export function useUpdateGroupAutoGroupConfig() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: GroupAutoGroupConfigUpdateRequest) =>
            apiClient.put<GroupAutoGroupConfig>('/api/v1/group/auto-group/config', data),
        onSuccess: (data) => {
            logger.log('自动分组配置已更新:', data);
            queryClient.setQueryData(['groups', 'auto-group', 'config'], data);
            invalidateAutoGroupRelated(queryClient);
        },
        onError: (error) => {
            logger.error('自动分组配置更新失败:', error);
        },
    });
}

export function useRunGroupAutoGroup() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async (data: GroupAutoGroupRunRequest = {}) =>
            apiClient.post<null>('/api/v1/group/auto-group/run', data),
        onSuccess: () => {
            logger.log('自动分组执行成功');
            invalidateAutoGroupRelated(queryClient);
        },
        onError: (error) => {
            logger.error('自动分组执行失败:', error);
        },
    });
}

/**
 * 自动添加分组 item Hook
 *
 * 后端路由: POST /api/v1/group/auto-add-item
 * Body: { id: number }
 *
 * @example
 * const autoAdd = useAutoAddGroupItem();
 * autoAdd.mutate(1); // 为 groupId=1 自动添加匹配的 items
 */
// export function useAutoAddGroupItem() {
//     const queryClient = useQueryClient();

//     return useMutation({
//         mutationFn: async (groupId: number) => {
//             return apiClient.post<null>(`/api/v1/group/auto-add-item`, { id: groupId });
//         },
//         onSuccess: () => {
//             logger.log('自动添加分组 item 成功');
//             queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
//         },
//         onError: (error) => {
//             logger.error('自动添加分组 item 失败:', error);
//         },
//     });
// }

/**
 * 获取某个分组的预设列表
 */
export function useGroupPresetList(groupID: number | undefined) {
    return useQuery({
        queryKey: ['groups', 'presets', groupID],
        queryFn: async () => apiClient.get<GroupPreset[]>(`/api/v1/group/preset/list/${groupID}`),
        enabled: typeof groupID === 'number' && groupID > 0,
        refetchInterval: 30000,
    });
}

/**
 * 创建预设：服务端从分组当前实时状态取快照
 */
export function useCreateGroupPreset() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ groupID, name }: { groupID: number; name: string }) =>
            apiClient.post<GroupPreset>(`/api/v1/group/preset/create/${groupID}`, { name }),
        onSuccess: (_, vars) => {
            queryClient.invalidateQueries({ queryKey: ['groups', 'presets', vars.groupID] });
        },
        onError: (error) => logger.error('预设创建失败:', error),
    });
}

/**
 * 激活预设：用预设覆盖分组的实时配置
 */
export function useActivateGroupPreset() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ presetID }: { presetID: number; groupID?: number }) =>
            apiClient.post<string>(`/api/v1/group/preset/activate/${presetID}`, {}),
        onSuccess: (_, vars) => {
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
            if (vars.groupID) {
                queryClient.invalidateQueries({ queryKey: ['groups', 'presets', vars.groupID] });
            }
        },
        onError: (error) => logger.error('预设激活失败:', error),
    });
}

/**
 * 用当前实时配置覆盖已有预设
 */
export function useOverwriteGroupPreset() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ presetID }: { presetID: number; groupID?: number }) =>
            apiClient.post<GroupPreset>(`/api/v1/group/preset/overwrite/${presetID}`, {}),
        onSuccess: (_, vars) => {
            if (vars.groupID) {
                queryClient.invalidateQueries({ queryKey: ['groups', 'presets', vars.groupID] });
            }
        },
        onError: (error) => logger.error('预设覆盖失败:', error),
    });
}

/**
 * 直接编辑预设内容（活动预设会被后端拒绝）
 */
export function useUpdateGroupPreset() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ presetID, data }: { presetID: number; groupID?: number; data: GroupPresetUpdateRequest }) =>
            apiClient.put<GroupPreset>(`/api/v1/group/preset/update/${presetID}`, data),
        onSuccess: (_, vars) => {
            if (vars.groupID) {
                queryClient.invalidateQueries({ queryKey: ['groups', 'presets', vars.groupID] });
            }
        },
        onError: (error) => logger.error('预设编辑失败:', error),
    });
}

/**
 * 重命名预设
 */
export function useRenameGroupPreset() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ presetID, name }: { presetID: number; groupID?: number; name: string }) =>
            apiClient.put<string>(`/api/v1/group/preset/rename/${presetID}`, { name }),
        onSuccess: (_, vars) => {
            if (vars.groupID) {
                queryClient.invalidateQueries({ queryKey: ['groups', 'presets', vars.groupID] });
            }
        },
        onError: (error) => logger.error('预设重命名失败:', error),
    });
}

/**
 * 删除预设（活动预设会被后端拒绝）
 */
export function useDeleteGroupPreset() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ presetID }: { presetID: number; groupID?: number }) =>
            apiClient.delete<string>(`/api/v1/group/preset/delete/${presetID}`),
        onSuccess: (_, vars) => {
            if (vars.groupID) {
                queryClient.invalidateQueries({ queryKey: ['groups', 'presets', vars.groupID] });
            }
        },
        onError: (error) => logger.error('预设删除失败:', error),
    });
}

/**
 * 切换分组置顶
 */
export function useToggleGroupPin() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ groupID, pinned }: { groupID: number; pinned: boolean }) =>
            apiClient.post<string>(`/api/v1/group/pin/${groupID}`, { pinned }),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['groups', 'list'] });
        },
        onError: (error) => logger.error('置顶切换失败:', error),
    });
}

