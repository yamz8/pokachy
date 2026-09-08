export type AppEnv = Env & {
  BETTER_AUTH_SECRET?: string;
  GITHUB_CLIENT_ID?: string;
  GITHUB_CLIENT_SECRET?: string;
  TURNSTILE_SECRET?: string;
};
export type MailJob = { email: string; otp: string; expiresAt: number };
export function isLocal(env: AppEnv): boolean {
  const url = new URL(env.BASE_URL);
  return env.ENVIRONMENT === "local" && url.protocol === "http:" && ["localhost", "127.0.0.1"].includes(url.hostname);
}
