'use client';

import { useEffect, useRef, useState } from 'react';
import { Eye, EyeOff, Loader2, Share2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Activity } from './activity';
import { StatsChart } from './chart';
import { GroupHealthSummaryStrip } from './group-health-summary-strip';
import { Rank } from './rank';
import { useHomeViewStore } from './store';
import { PageWrapper } from '@/components/common/PageWrapper';
import { ShareImagePreview, captureShareImage, type ShareImage } from '@/components/common/ShareImagePreview';
import { toast } from '@/components/common/Toast';
import Logo from '@/components/modules/logo';
import { buttonVariants } from '@/components/ui/button';

export function Home() {
    return (
        <PageWrapper className="h-full min-h-0 overflow-y-auto overscroll-contain space-y-6 pb-24 md:pb-4 rounded-t-3xl">
            <StatsChart />
            <GroupHealthSummaryStrip />
            <Activity />
            <Rank />
        </PageWrapper>
    );
}

const ACTION_BUTTON_CLASS = buttonVariants({
    variant: 'ghost',
    size: 'icon',
    className: 'rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground disabled:opacity-50',
});

// 顶栏里的两个首页按钮：模糊渠道名、导出总览分享图
export function HomeActions() {
    const t = useTranslations('common.share');
    const isChannelNameHidden = useHomeViewStore((state) => state.isChannelNameHidden);
    const setChannelNameHidden = useHomeViewStore((state) => state.setChannelNameHidden);
    const [isStaged, setIsStaged] = useState(false);
    const [image, setImage] = useState<ShareImage | null>(null);
    const stageRef = useRef<HTMLDivElement>(null);

    // 分享时在屏幕外另挂一份定宽的首页内容，等它的入场动画跑完再截图
    useEffect(() => {
        if (!isStaged) return;

        let cancelled = false;
        // snapdom 只截当前 DOM，不会推进动画：趋势图 1500ms、数字 800ms，留 300ms 余量
        const timer = window.setTimeout(async () => {
            if (cancelled || !stageRef.current) return;
            try {
                const next = await captureShareImage(stageRef.current);
                if (cancelled) URL.revokeObjectURL(next.url);
                else setImage(next);
            } catch {
                if (!cancelled) toast.error(t('failed'));
            } finally {
                if (!cancelled) setIsStaged(false);
            }
        }, 1800);

        return () => {
            cancelled = true;
            window.clearTimeout(timer);
        };
    }, [isStaged, t]);

    return (
        <div className="flex items-center gap-2">
            <button
                type="button"
                onClick={() => setChannelNameHidden(!isChannelNameHidden)}
                aria-pressed={isChannelNameHidden}
                aria-label={t('hideChannelName')}
                title={t('hideChannelName')}
                className={ACTION_BUTTON_CLASS}
            >
                {isChannelNameHidden ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
            <button
                type="button"
                onClick={() => setIsStaged(true)}
                disabled={isStaged}
                aria-label={t('share')}
                title={t('share')}
                className={ACTION_BUTTON_CLASS}
            >
                {isStaged ? <Loader2 className="size-4 animate-spin" /> : <Share2 className="size-4" />}
            </button>

            {isStaged && (
                <div
                    ref={stageRef}
                    aria-hidden="true"
                    className="fixed top-0 space-y-6 rounded-3xl bg-background p-4 text-foreground"
                    // 1002px 是热力图刚好铺满不出横向滚动条的宽度：54 列 × 14px + 53 × 4px 间隙 = 968，
                    // 加滚动容器左右各 16px 内边距和卡片左右各 1px 边框。content-box 让这个宽度落在内容上。
                    style={{ left: '-10000px', boxSizing: 'content-box', width: '1002px' }}
                >
                    <div className="flex items-center gap-x-2 px-2">
                        <Logo size={48} />
                        <span className="text-3xl font-bold">Octopus</span>
                    </div>
                    {/* 分组健康条不进分享图 */}
                    <StatsChart />
                    <Activity />
                    <Rank />
                </div>
            )}

            <ShareImagePreview image={image} onClose={() => setImage(null)} />
        </div>
    );
}
