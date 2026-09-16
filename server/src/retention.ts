import type { AppEnv } from "./env";

// Bounded batches avoid an unbounded maintenance invocation after a long outage.
// Repeated hourly runs drain any backlog; valid sessions/codes are never selected.
export async function cleanExpiredAuth(env: AppEnv, now = Date.now()) {
  const results = await env.DB.batch([
    env.DB.prepare("DELETE FROM session WHERE id IN (SELECT id FROM session WHERE expiresAt<=? LIMIT 1000)").bind(now),
    env.DB.prepare("DELETE FROM verification WHERE id IN (SELECT id FROM verification WHERE expiresAt<=? LIMIT 1000)").bind(now),
    env.DB.prepare("DELETE FROM deviceCode WHERE id IN (SELECT id FROM deviceCode WHERE expiresAt<=? LIMIT 1000)").bind(now),
    env.DB.prepare("DELETE FROM rateLimit WHERE id IN (SELECT id FROM rateLimit WHERE lastRequest<? LIMIT 1000)").bind(now - 24 * 60 * 60_000),
    env.DB.prepare("DELETE FROM dev_mail WHERE email IN (SELECT email FROM dev_mail WHERE expires_at<=? LIMIT 1000)").bind(now),
    env.DB.prepare("DELETE FROM account_handoffs WHERE token_hash IN (SELECT token_hash FROM account_handoffs WHERE expires_at<=? LIMIT 1000)").bind(now),
  ]);
  console.log(JSON.stringify({ event: "expired_auth_cleanup", deleted: results.map(result => result.meta.changes) }));
}
