'use client';

import type { ReactNode } from 'react';
import { NextIntlClientProvider } from 'next-intl';
import { useSettingStore, type Locale } from '@/stores/setting';

import zh_hansMessages from '../../public/locale/zh_hans.json';
import zh_hantMessages from '../../public/locale/zh_hant.json';
import enMessages from '../../public/locale/en.json';

const messages: Record<Locale, typeof zh_hansMessages> = {
    zh_hans: zh_hansMessages,
    zh_hant: zh_hantMessages,
    en: enMessages,
};

export function LocaleProvider({ children }: { children: ReactNode }) {
    const { locale } = useSettingStore();

    return (
        <NextIntlClientProvider
            locale={locale}
            messages={messages[locale]}
            timeZone="Asia/Shanghai"
        >
            {children}
        </NextIntlClientProvider>
    );
}
