import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { vendorOption } from './vendor-options';

/**
 * 厂商标签。vendor 为空表示未能识别，交由调用方决定是否渲染占位。
 */
export function VendorBadge({
    vendor,
    unknownLabel,
    className,
}: {
    vendor: string;
    unknownLabel: string;
    className?: string;
}) {
    const option = vendorOption(vendor);
    if (!option) {
        return (
            <Badge variant="outline" className={cn('border-dashed text-muted-foreground', className)}>
                {unknownLabel}
            </Badge>
        );
    }
    return (
        <Badge variant="secondary" className={cn('border-transparent', option.className, className)}>
            {option.label}
        </Badge>
    );
}
