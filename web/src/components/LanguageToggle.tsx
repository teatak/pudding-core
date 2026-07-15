import { Languages } from "lucide-react";

import {
  AppDropdownMenuContent as DropdownMenuContent,
  AppDropdownMenuRadioItem as DropdownMenuRadioItem,
} from "@/components/AppMenu";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuRadioGroup,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useI18n, type Locale } from "@/i18n";

export function LanguageToggle({ onOpenChange }: { onOpenChange?: (open: boolean) => void }) {
  const { locale, setLocale, t } = useI18n();
  const options: Array<{ value: Locale; label: string }> = [
    { value: "zh-CN", label: t("language.zhCN") },
    { value: "zh-TW", label: t("language.zhTW") },
    { value: "en", label: t("language.en") },
  ];

  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button aria-label={t("language.toggle")} size="icon" tabIndex={-1} variant="ghost">
          <Languages />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-32">
        <DropdownMenuRadioGroup value={locale} onValueChange={(value) => setLocale(value as Locale)}>
          {options.map((option) => (
            <DropdownMenuRadioItem key={option.value} value={option.value}>
              {option.label}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
