// Bindings, vars, and secrets come from `wrangler types`; the deploy workflow adds DEPLOY_REVISION.
export type AppEnv = Env & { DEPLOY_REVISION?: string };
export type MailJob = { email: string; otp: string; expiresAt: number; type?: "sign-in" | "email-verification" | "forget-password" | "change-email" };
// Production sets ADMIN_USER_IDS as a secret; local development leaves it unset.
export function adminIds(env: AppEnv): string[] {
  return (env.ADMIN_USER_IDS ?? "").split(",").map(value => value.trim()).filter(Boolean);
}
export function isLocal(env: AppEnv): boolean {
  const url = new URL(env.BASE_URL);
  return env.ENVIRONMENT === "local" && url.protocol === "http:" && ["localhost", "127.0.0.1"].includes(url.hostname);
}
