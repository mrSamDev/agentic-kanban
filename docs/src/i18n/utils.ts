import { ui, defaultLang, type Lang } from './ui';

export function getLangFromUrl(url: URL): Lang {
  const [, base, lang] = url.pathname.split('/');
  // URL pattern: /agentic-kanban/zh/... or /agentic-kanban/
  if (lang && lang in ui) return lang as Lang;
  return defaultLang;
}

export function useTranslations(lang: Lang) {
  return function t(key: keyof typeof ui[typeof defaultLang]): string {
    return (ui[lang]?.[key] ?? ui[defaultLang][key]) as string;
  };
}
