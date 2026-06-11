import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useI18n } from "@/i18n";
import { setToken } from "@/state/tokenStore";

export function TokenGate() {
  const [value, setValue] = useState("");
  const { t } = useI18n();

  return (
    <main className="flex min-h-svh items-center justify-center bg-background p-6">
      <form
        className="w-full max-w-sm"
        onSubmit={(event) => {
          event.preventDefault();
          setToken(value);
        }}
      >
        <Card>
          <CardHeader>
            <CardTitle>{t("app.core")}</CardTitle>
            <CardDescription>{t("token.description")}</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid gap-2">
              <Label htmlFor="daemon-token">{t("token.label")}</Label>
              <Input
                id="daemon-token"
                autoFocus
                value={value}
                onChange={(event) => setValue(event.target.value)}
                placeholder={t("token.placeholder")}
              />
            </div>
          </CardContent>
          <CardFooter>
            <Button className="w-full" type="submit" disabled={!value.trim()}>
              {t("token.continue")}
            </Button>
          </CardFooter>
        </Card>
      </form>
    </main>
  );
}
