'use client';

import { useEffect } from 'react';
import { Copy, Download, X } from 'lucide-react';
import { snapdom } from '@zumer/snapdom';
import { useTranslations } from 'next-intl';
import dayjs from 'dayjs';
import { toast } from '@/components/common/Toast';

export type ShareImage = { url: string; blob: Blob };

// 把节点截成 PNG。带 data-share-exclude 的子节点不进图片（地址、密钥、操作按钮）。
export async function captureShareImage(node: HTMLElement): Promise<ShareImage> {
    // snapdom 不解析 SVG <defs> 里的 var()，先换成算好的颜色，否则渐变截出来是黑的
    node.querySelectorAll('stop').forEach((stop) => stop.setAttribute('stop-color', getComputedStyle(stop).stopColor));
    const blob = await snapdom.toBlob(node, {
        type: 'png',
        scale: 2,
        embedFonts: true,
        exclude: ['[data-share-exclude]'],
        excludeMode: 'remove',
    });
    return { url: URL.createObjectURL(blob), blob };
}

const BUTTON_CLASS = 'flex size-10 items-center justify-center rounded-xl border border-border bg-card text-muted-foreground transition-colors hover:text-foreground active:scale-95';

// 分享图预览：复制到剪贴板 / 下载 / 关闭。一直挂载，image 为空时不渲染。
export function ShareImagePreview({ image, onClose }: { image: ShareImage | null; onClose: () => void }) {
    const t = useTranslations('common');

    // 换图、关闭或卸载时释放临时对象 URL
    useEffect(() => () => {
        if (image) URL.revokeObjectURL(image.url);
    }, [image]);

    if (!image) return null;

    const handleCopy = async () => {
        try {
            await navigator.clipboard.write([new ClipboardItem({ 'image/png': image.blob })]);
            toast.success(t('copy.success'));
        } catch {
            toast.error(t('copy.failed'));
        }
    };

    const handleDownload = () => {
        const link = document.createElement('a');
        link.href = image.url;
        link.download = `octopus-${dayjs().format('YYYYMMDD')}.png`;
        link.click();
    };

    return (
        <div className="fixed inset-0 z-60 flex flex-col items-center justify-center gap-3 rounded-[inherit] bg-background/95 p-4 backdrop-blur-sm">
            {/* eslint-disable-next-line @next/next/no-img-element -- 本地生成的 blob 图，用不上 next/image */}
            <img
                src={image.url}
                alt={t('share.share')}
                className="max-h-[75vh] min-h-0 w-auto max-w-full rounded-2xl border border-border object-contain"
            />
            <div className="flex shrink-0 items-center gap-2">
                <button type="button" onClick={handleCopy} aria-label={t('share.copy')} title={t('share.copy')} className={BUTTON_CLASS}>
                    <Copy className="size-4" />
                </button>
                <button type="button" onClick={handleDownload} aria-label={t('share.download')} title={t('share.download')} className={BUTTON_CLASS}>
                    <Download className="size-4" />
                </button>
                <button type="button" onClick={onClose} aria-label={t('share.close')} title={t('share.close')} className={BUTTON_CLASS}>
                    <X className="size-4" />
                </button>
            </div>
        </div>
    );
}
