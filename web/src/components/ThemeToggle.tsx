import { Monitor, Moon, Sun } from "@/components/icons";

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
import { useI18n } from "@/i18n";
import { setTheme, useTheme, type Theme } from "@/theme/theme";

export function ThemeToggle({ onOpenChange }: { onOpenChange?: (open: boolean) => void }) {
  const { t } = useI18n();
  const { theme, resolved } = useTheme();
  const Icon = resolved === "dark" ? Moon : Sun;
  const options: Array<{ value: Theme; label: string; icon: typeof Sun }> = [
    { value: "light", label: t("theme.light"), icon: Sun },
    { value: "dark", label: t("theme.dark"), icon: Moon },
    { value: "system", label: t("theme.system"), icon: Monitor },
  ];

  return (
    <DropdownMenu onOpenChange={onOpenChange}>
      <DropdownMenuTrigger asChild>
        <Button aria-label={t("theme.toggle")} size="icon" tabIndex={-1} variant="ghost">
          <Icon />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-36">
        <DropdownMenuRadioGroup value={theme} onValueChange={(value) => setTheme(value as Theme)}>
          {options.map(({ value, label, icon: OptionIcon }) => (
            <DropdownMenuRadioItem key={value} value={value}>
              <OptionIcon />
              <span>{label}</span>
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
