"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { QRCodeSVG } from "qrcode.react";

import { api, ApiError } from "@/lib/api";
import { S, errorText } from "@/lib/strings";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Operator login. TOTP is mandatory: the flow has 3 states — credentials, enrollment
// (1st login: shows the secret/otpauth and asks for the 1st code) and verification (the usual code).
type Step = "credentials" | "enroll" | "verify";

export default function LoginPage() {
  const router = useRouter();
  const [step, setStep] = useState<Step>("credentials");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [setup, setSetup] = useState<{ secret: string; otpauth_uri: string } | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (step !== "credentials" && code.trim().length !== 6) {
      setError(S.login.codeRequired);
      return;
    }
    setError("");
    setBusy(true);
    try {
      const res = await api.login(email, password, step === "credentials" ? "" : code.trim());
      if ("totp_setup" in res) {
        setSetup({ secret: res.secret, otpauth_uri: res.otpauth_uri });
        setStep("enroll");
      } else {
        router.replace("/");
      }
    } catch (err) {
      const code = err instanceof ApiError ? err.code : "manager.failed";
      if (code === "manager.totp_code_required") {
        setStep("verify"); // password ok; the 2nd factor is still missing
      } else {
        setError(errorText(code));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-muted/40 p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>{step === "enroll" ? S.login.totpSetupTitle : S.login.title}</CardTitle>
          <CardDescription>
            {step === "enroll" ? S.login.totpSetupHelp : S.login.subtitle}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="flex flex-col gap-4">
            {step === "credentials" && (
              <>
                <div className="grid gap-2">
                  <Label htmlFor="email">{S.login.email}</Label>
                  <Input id="email" type="email" autoComplete="username" autoFocus required
                    value={email} onChange={(e) => setEmail(e.target.value)} />
                </div>
                <div className="grid gap-2">
                  <Label htmlFor="password">{S.login.password}</Label>
                  <Input id="password" type="password" autoComplete="current-password" required
                    value={password} onChange={(e) => setPassword(e.target.value)} />
                </div>
              </>
            )}

            {step === "enroll" && setup && (
              <div className="grid gap-3 rounded-md border bg-muted/50 p-3 text-sm">
                {/* otpauth:// QR: point the authenticator app's camera at it. Fixed white background for
                    contrast in the dark theme too. */}
                <div className="flex justify-center rounded-md bg-white p-3">
                  <QRCodeSVG value={setup.otpauth_uri} size={168} marginSize={1} />
                </div>
                <div className="text-muted-foreground">{S.login.totpSecret}</div>
                <code className="break-all font-mono text-xs">{setup.secret}</code>
              </div>
            )}

            {step !== "credentials" && (
              <div className="grid gap-2">
                <Label htmlFor="code">{S.login.code}</Label>
                <Input id="code" inputMode="numeric" pattern="[0-9]*" maxLength={6}
                  autoComplete="one-time-code" autoFocus required
                  value={code} onChange={(e) => setCode(e.target.value)} />
              </div>
            )}

            {error && <p className="text-sm text-destructive">{error}</p>}

            <Button type="submit" disabled={busy}>
              {busy ? S.login.verifying : step === "enroll" ? S.login.totpConfirm : S.login.submit}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}
