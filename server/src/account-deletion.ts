import { Hono } from "hono";
import { createAuth } from "./auth";
import type { AppEnv } from "./env";

export const accountRoutes = new Hono<{ Bindings: AppEnv }>();

accountRoutes.post("/email-change/current-code", async c => {
  const auth = createAuth(c.env);
  const identity = await auth.api.getSession({ headers: c.req.raw.headers });
  if (!identity) return c.json({ error: "Sign in to Pokachy" }, 401);
  if (!identity.user.emailVerified) return c.json({ error: "Verify your email first" }, 403);
  if (!(await c.env.HUBS.getByName(`email-change:${identity.user.id}`).consume("current-code", 3, 300_000))) {
    return c.json({ error: "Please wait five minutes before requesting another verification code." }, 429);
  }
  await auth.api.sendVerificationOTP({ body: { email: identity.user.email, type: "email-verification" } });
  return c.json({ ok: true });
});

accountRoutes.post("/deletion-code", async c => {
  const auth = createAuth(c.env);
  const identity = await auth.api.getSession({ headers: c.req.raw.headers });
  if (!identity) return c.json({ error: "Sign in to Pokachy" }, 401);
  if (!identity.user.emailVerified) return c.json({ error: "Verify your email first" }, 403);
  if (!(await c.env.HUBS.getByName(`deletion:${identity.user.id}`).consume("send", 3, 300_000))) {
    return c.json({ error: "Please wait five minutes before requesting another deletion code." }, 429);
  }
  await auth.api.sendVerificationOTP({ body: { email: identity.user.email, type: "email-verification" } });
  return c.json({ ok: true });
});

// Registered before the suspension gate: suspended users can still erase their account.
accountRoutes.post("/delete", async c => {
  const auth = createAuth(c.env);
  const identity = await auth.api.getSession({ headers: c.req.raw.headers });
  if (!identity) return c.json({ error: "Sign in to Pokachy" }, 401);
  if (!identity.user.emailVerified) return c.json({ error: "Verify your email first" }, 403);
  const age = Date.now() - identity.session.createdAt.getTime();
  if (age < 0 || age > 5 * 60_000) {
    return c.json({ error: "Sign out and sign in again, then delete your account within five minutes." }, 403);
  }
  const { confirmation, otp } = await c.req.json<{ confirmation?: unknown; otp?: unknown }>();
  if (confirmation !== "DELETE MY ACCOUNT") return c.json({ error: "Type DELETE MY ACCOUNT to confirm." }, 400);
  const id = identity.user.id;
  if (c.env.ADMIN_USER_IDS.split(",").map(value => value.trim()).includes(id)) {
    return c.json({ error: "Transfer administrator responsibility and remove this account from administrator configuration before deleting it." }, 409);
  }
  // A recently approved device also has a fresh session. Require independent email
  // possession so an old stolen device token cannot manufacture deletion authority.
  if (typeof otp !== "string" || !/^\d{6}$/.test(otp)) return c.json({ error: "Enter the six-digit deletion code from your email." }, 400);
  if (!(await c.env.HUBS.getByName(`deletion:${id}`).consume("verify", 5, 300_000))) {
    return c.json({ error: "Too many deletion attempts. Please wait five minutes." }, 429);
  }
  try {
    await auth.api.checkVerificationOTP({ body: { email: identity.user.email, type: "email-verification", otp } });
  } catch {
    return c.json({ error: "Invalid or expired deletion code. Request another code and try again." }, 400);
  }
  const friends = await c.env.DB.prepare("SELECT CASE WHEN a=? THEN b ELSE a END AS id FROM friendships WHERE a=? OR b=?")
    .bind(id, id, id).all<{ id: string }>();
  const profile = await c.env.DB.prepare("SELECT avatar_key FROM profiles WHERE user_id=?").bind(id).first<{ avatar_key: string | null }>();
  const email = identity.user.email.toLowerCase();
  // A D1 batch is atomic. Explicitly remove requester references before deleting the user;
  // remaining account/session/profile/poke/block/report foreign keys cascade.
  await c.env.DB.batch([
    c.env.DB.prepare("DELETE FROM friendships WHERE a=? OR b=? OR requester=?").bind(id, id, id),
    c.env.DB.prepare("DELETE FROM deviceCode WHERE userId=?").bind(id),
    c.env.DB.prepare("DELETE FROM verification WHERE identifier IN (?,?,?,?)")
      .bind(`sign-in-otp-${email}`, `email-verification-otp-${email}`, `forget-password-otp-${email}`, `change-email-otp-${email}`),
    c.env.DB.prepare("DELETE FROM dev_mail WHERE email=?").bind(email),
    c.env.DB.prepare("DELETE FROM user WHERE id=?").bind(id),
  ]);
  if (profile?.avatar_key) {
    await c.env.AVATARS.delete(profile.avatar_key).catch(() => {
      console.warn(JSON.stringify({ event: "account_avatar_cleanup_failed" }));
    });
  }
  // Revalidate open sockets and refresh friends without returning deleted profile data.
  const ids = [id, ...friends.results.map(friend => friend.id)];
  for (let start = 0; start < ids.length; start += 20) {
    await Promise.all(ids.slice(start, start + 20).map(userId =>
      c.env.HUBS.getByName(`user:${userId}`).publish().catch(() => {
        console.warn(JSON.stringify({ event: "account_deletion_sync_failed" }));
      })));
  }
  return c.json({ ok: true });
});
