'use client';

import { useMemo, useRef, useState } from 'react';
import { X } from 'lucide-react';
import { toast } from 'sonner';

import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';

const TAG_MAX_LENGTH = 32;
const TAGS_MAX_COUNT = 20;

type TagInputProps = {
    value: string[];
    onChange: (tags: string[]) => void;
    suggestions?: string[];
    placeholder?: string;
    className?: string;
};

export function TagInput({
    value,
    onChange,
    suggestions = [],
    placeholder = '输入标签后回车',
    className,
}: TagInputProps) {
    const [draft, setDraft] = useState('');
    const [focused, setFocused] = useState(false);
    const inputRef = useRef<HTMLInputElement>(null);

    const matchedSuggestions = useMemo(() => {
        const keyword = draft.trim().toLowerCase();
        return suggestions.filter(
            (tag) =>
                !value.includes(tag) &&
                (keyword === '' || tag.toLowerCase().includes(keyword)),
        );
    }, [suggestions, value, draft]);

    function addTag(raw: string) {
        const tag = raw.trim();
        if (!tag) return;
        if (tag.length > TAG_MAX_LENGTH) {
            toast.error(`单个标签不能超过 ${TAG_MAX_LENGTH} 个字符`);
            return;
        }
        if (value.includes(tag)) {
            setDraft('');
            return;
        }
        if (value.length >= TAGS_MAX_COUNT) {
            toast.error(`标签数量不能超过 ${TAGS_MAX_COUNT} 个`);
            return;
        }
        onChange([...value, tag]);
        setDraft('');
    }

    function removeTag(tag: string) {
        onChange(value.filter((item) => item !== tag));
    }

    function handleKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
        if (event.key === 'Enter' || event.key === ',' || event.key === '，') {
            event.preventDefault();
            addTag(draft);
            return;
        }
        if (event.key === 'Backspace' && draft === '' && value.length > 0) {
            removeTag(value[value.length - 1]);
        }
    }

    return (
        <div className={cn('relative', className)}>
            <div
                className="flex min-h-9 w-full cursor-text flex-wrap items-center gap-1.5 rounded-xl border border-input bg-transparent px-3 py-1.5 text-sm transition-[color,box-shadow] focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/50"
                onClick={() => inputRef.current?.focus()}
            >
                {value.map((tag) => (
                    <Badge key={tag} variant="secondary" className="gap-1 pr-1">
                        {tag}
                        <button
                            type="button"
                            onClick={(event) => {
                                event.stopPropagation();
                                removeTag(tag);
                            }}
                            className="rounded-full p-0.5 transition-colors hover:bg-muted-foreground/20"
                            aria-label={`移除标签 ${tag}`}
                        >
                            <X className="size-3" />
                        </button>
                    </Badge>
                ))}
                <input
                    ref={inputRef}
                    value={draft}
                    onChange={(event) => setDraft(event.target.value)}
                    onKeyDown={handleKeyDown}
                    onFocus={() => setFocused(true)}
                    onBlur={() => {
                        setFocused(false);
                        addTag(draft);
                    }}
                    placeholder={value.length === 0 ? placeholder : ''}
                    className="min-w-20 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
                />
            </div>
            {focused && matchedSuggestions.length > 0 ? (
                <div className="absolute top-full left-0 z-50 mt-1 max-h-48 w-full overflow-auto rounded-xl border border-border bg-popover p-1 shadow-md">
                    {matchedSuggestions.map((tag) => (
                        <button
                            key={tag}
                            type="button"
                            onMouseDown={(event) => {
                                event.preventDefault();
                                addTag(tag);
                            }}
                            className="flex w-full items-center rounded-lg px-2 py-1.5 text-left text-sm transition-colors hover:bg-muted/60"
                        >
                            {tag}
                        </button>
                    ))}
                </div>
            ) : null}
        </div>
    );
}
