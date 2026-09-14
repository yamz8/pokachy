import { env } from "cloudflare:test";
import { beforeAll, expect, test } from "vitest";
import worker from "../src/index";
import { cleanExpiredAuth } from "../src/retention";
import migration from "../migrations/0001_initial.sql?raw";

const origin = "http://127.0.0.1:8787";
const ctx = { waitUntil: () => {}, passThroughOnException: () => {} };
let requestNumber = 0;
async function request(path: string, token?: string, method = "GET", body?: unknown, adminId?: string) {
  requestNumber++;
  const runtime = adminId ? { ...env, ADMIN_USER_IDS: adminId } as typeof env : env;
  return worker.fetch(new Request(origin + path, {
    method,
    headers: { "Content-Type": "application/json", "X-Pokachy-Client": "cli", "CF-Connecting-IP": `192.0.2.${requestNumber}`, ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body),
  }), runtime, ctx);
}
async function user(handle: string) {
  const email = `${handle}@example.test`;
  expect((await request("/api/auth/email-otp/send-verification-otp", undefined, "POST", { email, type: "sign-in" })).status).toBe(200);
  const { otp } = await (await request(`/api/dev/mail?email=${email}`)).json() as { otp: string };
  const response = await request("/api/auth/sign-in/email-otp", undefined, "POST", { email, otp, name: handle });
  const data = await response.json() as { token: string; user: { id: string } };
  expect(response.status).toBe(200);
  expect((await request("/api/profile", data.token, "PUT", { handle })).status).toBe(200);
  return { ...data, email };
}
async function deletionCode(token: string, email: string) {
  expect((await request("/api/account/deletion-code", token, "POST", {})).status).toBe(200);
  return (await (await request(`/api/dev/mail?email=${email}`)).json() as { otp: string }).otp;
}
const count = async (query: string, ...args: unknown[]) => Number((await env.DB.prepare(query).bind(...args).first<{ count: number }>())?.count ?? 0);

beforeAll(async () => { await env.DB.exec(migration); });

test("account deletion requires a recent verified, non-administrator session and confirmation", async () => {
  expect((await request("/api/account/delete", undefined, "POST", { confirmation: "DELETE MY ACCOUNT" })).status).toBe(401);

  const unverified = await user("deleteunverified");
  await env.DB.prepare("UPDATE user SET emailVerified=0 WHERE id=?").bind(unverified.user.id).run();
  expect((await request("/api/account/delete", unverified.token, "POST", { confirmation: "DELETE MY ACCOUNT" })).status).toBe(403);

  const stale = await user("deletestale");
  await env.DB.prepare("UPDATE session SET createdAt=? WHERE userId=?").bind(Date.now() - 300_001, stale.user.id).run();
  expect((await request("/api/account/delete", stale.token, "POST", { confirmation: "DELETE MY ACCOUNT" })).status).toBe(403);

  const wrongConfirmation = await user("deleteconfirm");
  expect((await request("/api/account/delete", wrongConfirmation.token, "POST", { confirmation: "delete" })).status).toBe(400);
  expect(await env.DB.prepare("SELECT 1 FROM user WHERE id=?").bind(wrongConfirmation.user.id).first()).not.toBeNull();

  const missingCode = await user("deletemissingcode");
  expect((await request("/api/account/delete", missingCode.token, "POST", { confirmation: "DELETE MY ACCOUNT" })).status).toBe(400);
  expect(await env.DB.prepare("SELECT 1 FROM user WHERE id=?").bind(missingCode.user.id).first()).not.toBeNull();

  const wrongCode = await user("deletewrongcode");
  const validCode = await deletionCode(wrongCode.token, wrongCode.email);
  const invalidCode = validCode === "000000" ? "000001" : "000000";
  expect((await request("/api/account/delete", wrongCode.token, "POST", { confirmation: "DELETE MY ACCOUNT", otp: invalidCode })).status).toBe(400);
  expect(await env.DB.prepare("SELECT 1 FROM user WHERE id=?").bind(wrongCode.user.id).first()).not.toBeNull();

  const sendLimited = await user("deletesendlimit");
  for (let attempt = 0; attempt < 3; attempt++) expect((await request("/api/account/deletion-code", sendLimited.token, "POST", {})).status).toBe(200);
  expect((await request("/api/account/deletion-code", sendLimited.token, "POST", {})).status).toBe(429);

  const attemptsLimited = await user("deleteattemptlimit");
  const limitedCode = await deletionCode(attemptsLimited.token, attemptsLimited.email);
  const invalidLimitedCode = limitedCode === "000000" ? "000001" : "000000";
  for (let attempt = 0; attempt < 5; attempt++) expect((await request("/api/account/delete", attemptsLimited.token, "POST", { confirmation: "DELETE MY ACCOUNT", otp: invalidLimitedCode })).status).toBe(400);
  expect((await request("/api/account/delete", attemptsLimited.token, "POST", { confirmation: "DELETE MY ACCOUNT", otp: invalidLimitedCode })).status).toBe(429);
  expect(await env.DB.prepare("SELECT 1 FROM user WHERE id=?").bind(attemptsLimited.user.id).first()).not.toBeNull();

  const administrator = await user("deleteadmin");
  expect((await request("/api/account/delete", administrator.token, "POST", { confirmation: "DELETE MY ACCOUNT" }, administrator.user.id)).status).toBe(409);
  expect(await env.DB.prepare("SELECT 1 FROM user WHERE id=?").bind(administrator.user.id).first()).not.toBeNull();
});

test("active and suspended accounts delete related records while preserving other accounts", async () => {
  const owner = await user("deleteowner");
  const peer = await user("deletepeer");
  const [a, b] = [owner.user.id, peer.user.id].sort();
  const now = Date.now();
  const verificationIdentifiers = ["sign-in-otp", "email-verification-otp", "forget-password-otp", "change-email-otp"].map(kind => `${kind}-${owner.email}`);
  await env.DB.batch([
    env.DB.prepare("INSERT INTO friendships(a,b,requester,created_at) VALUES (?,?,?,?)").bind(a, b, owner.user.id, now),
    env.DB.prepare("INSERT INTO blocks(blocker,blocked) VALUES (?,?),(?,?)").bind(owner.user.id, peer.user.id, peer.user.id, owner.user.id),
    env.DB.prepare("INSERT INTO pokes(id,sender,recipient,created_at,request_key) VALUES (?,?,?,?,?),(?,?,?,?,?)")
      .bind("owner-poke", owner.user.id, peer.user.id, now, "owner-key", "peer-poke", peer.user.id, owner.user.id, now, "peer-key"),
    env.DB.prepare("INSERT INTO reports(id,reporter,reported,reason,created_at) VALUES (?,?,?,?,?),(?,?,?,?,?)")
      .bind("owner-report", owner.user.id, peer.user.id, "Owner report", now, "peer-report", peer.user.id, owner.user.id, "Peer report", now),
    env.DB.prepare("INSERT INTO account(id,accountId,providerId,userId,createdAt,updatedAt) VALUES (?,?,?,?,?,?)").bind("owner-account", "owner", "test", owner.user.id, now, now),
    env.DB.prepare("INSERT INTO deviceCode(id,deviceCode,userCode,userId,expiresAt,status) VALUES (?,?,?,?,?,?)").bind("owner-device", "owner-device-code", "OWNERDEV", owner.user.id, now + 60_000, "pending"),
    ...verificationIdentifiers.map((identifier, index) => env.DB.prepare("INSERT INTO verification(id,identifier,value,expiresAt,createdAt,updatedAt) VALUES (?,?,?,?,?,?)")
      .bind(`owner-verification-${index}`, identifier, "value", now + 60_000, now, now)),
    env.DB.prepare("INSERT INTO dev_mail(email,otp,expires_at) VALUES (?,?,?) ON CONFLICT(email) DO UPDATE SET otp=excluded.otp,expires_at=excluded.expires_at").bind(owner.email, "123456", now + 60_000),
  ]);

  const events = await worker.fetch(new Request(origin + "/api/events", { headers: { "Content-Type": "application/json", "X-Pokachy-Client": "cli", "CF-Connecting-IP": "192.0.2.250", Authorization: `Bearer ${owner.token}`, Upgrade: "websocket" } }), env, ctx);
  expect(events.status).toBe(101);
  const socket = events.webSocket!;
  let closed = false;
  socket.addEventListener("close", () => { closed = true; });
  socket.accept();
  const code = await deletionCode(owner.token, owner.email);
  expect((await request("/api/account/delete", owner.token, "POST", { confirmation: "DELETE MY ACCOUNT", otp: code })).status).toBe(200);
  await expect.poll(() => closed).toBe(true);
  expect((await request("/api/state", owner.token)).status).toBe(401);
  expect(await count("SELECT COUNT(*) AS count FROM user WHERE id=?", owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM profiles WHERE user_id=?", owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM session WHERE userId=?", owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM account WHERE userId=?", owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM deviceCode WHERE userId=?", owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM friendships WHERE a=? OR b=? OR requester=?", owner.user.id, owner.user.id, owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM blocks WHERE blocker=? OR blocked=?", owner.user.id, owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM pokes WHERE sender=? OR recipient=?", owner.user.id, owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM reports WHERE reporter=? OR reported=?", owner.user.id, owner.user.id)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM verification WHERE identifier IN (?,?,?,?)", ...verificationIdentifiers)).toBe(0);
  expect(await count("SELECT COUNT(*) AS count FROM dev_mail WHERE email=?", owner.email)).toBe(0);
  expect(await env.DB.prepare("SELECT 1 FROM user WHERE id=?").bind(peer.user.id).first()).not.toBeNull();

  const suspended = await user("deletesuspended");
  await env.DB.prepare("UPDATE profiles SET suspended=1 WHERE user_id=?").bind(suspended.user.id).run();
  const suspendedCode = await deletionCode(suspended.token, suspended.email);
  expect((await request("/api/account/delete", suspended.token, "POST", { confirmation: "DELETE MY ACCOUNT", otp: suspendedCode })).status).toBe(200);
  expect(await count("SELECT COUNT(*) AS count FROM user WHERE id=?", suspended.user.id)).toBe(0);
});

test("expired authentication cleanup is bounded and leaves valid records intact", async () => {
  const now = Date.now();
  const userId = "cleanup-user";
  await env.DB.prepare("INSERT INTO user(id,name,email,emailVerified,createdAt,updatedAt) VALUES (?,?,?,?,?,?)")
    .bind(userId, "Cleanup", "cleanup@example.test", 1, now, now).run();
  const expiredSessions = Array.from({ length: 1001 }, (_, index) => env.DB.prepare("INSERT INTO session(id,token,expiresAt,createdAt,updatedAt,userId) VALUES (?,?,?,?,?,?)")
    .bind(`expired-session-${index}`, `expired-token-${index}`, now - 1, now - 2, now - 2, userId));
  for (let start = 0; start < expiredSessions.length; start += 100) await env.DB.batch(expiredSessions.slice(start, start + 100));
  await env.DB.batch([
    env.DB.prepare("INSERT INTO session(id,token,expiresAt,createdAt,updatedAt,userId) VALUES (?,?,?,?,?,?)").bind("valid-session", "valid-token", now + 1, now, now, userId),
    env.DB.prepare("INSERT INTO verification(id,identifier,value,expiresAt,createdAt,updatedAt) VALUES (?,?,?,?,?,?)").bind("expired-verification", "expired", "v", now - 1, now, now),
    env.DB.prepare("INSERT INTO verification(id,identifier,value,expiresAt,createdAt,updatedAt) VALUES (?,?,?,?,?,?)").bind("valid-verification", "valid", "v", now + 1, now, now),
    env.DB.prepare("INSERT INTO deviceCode(id,deviceCode,userCode,userId,expiresAt,status) VALUES (?,?,?,?,?,?)").bind("expired-device", "expired-device-code", "EXPIRED", userId, now - 1, "pending"),
    env.DB.prepare("INSERT INTO deviceCode(id,deviceCode,userCode,userId,expiresAt,status) VALUES (?,?,?,?,?,?)").bind("valid-device", "valid-device-code", "VALID", userId, now + 1, "pending"),
    env.DB.prepare("INSERT INTO rateLimit(id,key,count,lastRequest) VALUES (?,?,?,?)").bind("expired-rate", "expired-rate", 1, now - 86_400_001),
    env.DB.prepare("INSERT INTO rateLimit(id,key,count,lastRequest) VALUES (?,?,?,?)").bind("valid-rate", "valid-rate", 1, now - 86_400_000),
    env.DB.prepare("INSERT INTO dev_mail(email,otp,expires_at) VALUES (?,?,?)").bind("expired-cleanup@example.test", "123456", now - 1),
    env.DB.prepare("INSERT INTO dev_mail(email,otp,expires_at) VALUES (?,?,?)").bind("valid-cleanup@example.test", "123456", now + 1),
  ]);

  await cleanExpiredAuth(env, now);
  expect(await count("SELECT COUNT(*) AS count FROM session WHERE id LIKE 'expired-session-%'")).toBe(1);
  expect(await env.DB.prepare("SELECT 1 FROM session WHERE id='valid-session'").first()).not.toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM verification WHERE id='expired-verification'").first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM verification WHERE id='valid-verification'").first()).not.toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM deviceCode WHERE id='expired-device'").first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM deviceCode WHERE id='valid-device'").first()).not.toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM rateLimit WHERE id='expired-rate'").first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM rateLimit WHERE id='valid-rate'").first()).not.toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM dev_mail WHERE email='expired-cleanup@example.test'").first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM dev_mail WHERE email='valid-cleanup@example.test'").first()).not.toBeNull();
});
