import { betterAuth } from "better-auth";
import { drizzleAdapter } from "better-auth/adapters/drizzle";
import { bearer, deviceAuthorization, emailOTP } from "better-auth/plugins";
import { drizzle } from "drizzle-orm/d1";
import * as schema from "./schema";
import { isLocal, type AppEnv, type MailJob } from "./env";

export function createAuth(env: AppEnv) {
  const local = isLocal(env);
  if (!local && !env.BETTER_AUTH_SECRET) throw new Error("BETTER_AUTH_SECRET is required");
  return betterAuth({
    appName: "Pokachy", baseURL: env.BASE_URL,
    secret: env.BETTER_AUTH_SECRET ?? "local-development-only-pokachy-secret-000000000000",
    database: drizzleAdapter(drizzle(env.DB, { schema }), { provider: "sqlite", schema, transaction: false }),
    trustedOrigins: [env.BASE_URL],
    session: { expiresIn: 60 * 60 * 24 * 30, updateAge: 60 * 60 * 24 },
    account: { encryptOAuthTokens: true, accountLinking: { enabled: true, disableImplicitLinking: true, requireLocalEmailVerified: true } },
    rateLimit: { enabled: true, storage: "database", window: 60, max: 60 },
    advanced: { useSecureCookies: !local, ipAddress: { ipAddressHeaders: ["cf-connecting-ip"] } },
    socialProviders: env.GITHUB_CLIENT_ID && env.GITHUB_CLIENT_SECRET ? {
      github: { clientId: env.GITHUB_CLIENT_ID, clientSecret: env.GITHUB_CLIENT_SECRET, scope: ["user:email"] },
    } : {},
    plugins: [
      bearer(),
      deviceAuthorization({ verificationUri: `${env.BASE_URL}/activate`, validateClient: (id) => id === "pokachy-cli" }),
      emailOTP({
        otpLength: 6, expiresIn: 300, allowedAttempts: 5, storeOTP: "hashed",
        async sendVerificationOTP({ email, otp }) {
          const job: MailJob = { email, otp, expiresAt: Date.now() + 300_000 };
          if (local) {
            await env.DB.prepare("INSERT INTO dev_mail VALUES (?, ?, ?) ON CONFLICT(email) DO UPDATE SET otp=excluded.otp, expires_at=excluded.expires_at").bind(email, otp, job.expiresAt).run();
          } else {
            if (!env.MAIL_QUEUE) throw new Error("MAIL_QUEUE is required");
            await env.MAIL_QUEUE.send(job);
          }
        },
      }),
    ],
  });
}
