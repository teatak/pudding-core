import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { setToken } from "@/state/tokenStore";

export function TokenGate() {
  const [value, setValue] = useState("");

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
            <CardTitle>Pudding Core</CardTitle>
            <CardDescription>Daemon token</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid gap-2">
              <Label htmlFor="daemon-token">Token</Label>
              <Input
                id="daemon-token"
                autoFocus
                value={value}
                onChange={(event) => setValue(event.target.value)}
                placeholder="Paste daemon token"
              />
            </div>
          </CardContent>
          <CardFooter>
            <Button className="w-full" type="submit" disabled={!value.trim()}>
              Continue
            </Button>
          </CardFooter>
        </Card>
      </form>
    </main>
  );
}
