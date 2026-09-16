import { Hono } from "hono";
import type { Context } from "hono";
import { createAuth } from "./auth";
import { accountRoutes } from "./account-deletion";
import { cleanExpiredAuth } from "./retention";
import { isLocal, type AppEnv, type MailJob } from "./env";
export { UserHub } from "./hub";

type Identity = NonNullable<Awaited<ReturnType<ReturnType<typeof createAuth>["api"]["getSession"]>>>;
type App = { Bindings: AppEnv; Variables: { identity: Identity } };
type Ctx = Context<App>;
const app = new Hono<App>();
const handlePattern = /^[a-z0-9][a-z0-9_]{2,23}$/;
const error = (c: Ctx, message: string, status: 400 | 401 | 403 | 404 | 409 | 429 | 503 = 400) => c.json({ error: message }, status);
const uid = (c: Ctx) => c.get("identity").user.id;
const pair = (a: string, b: string) => [a, b].sort();
const sql = (c: Ctx, query: string, ...args: unknown[]) => c.env.DB.prepare(query).bind(...args);
function githubAvatar(value: unknown) {
  if (typeof value !== "string" || value.length > 2048) return null;
  try {
    const url = new URL(value);
    return url.origin === "https://avatars.githubusercontent.com" && !url.username && !url.password ? url.href : null;
  } catch { return null; }
}
const withAvatar = <T extends Record<string, unknown>>(row: T): T & { image: string | null } => ({ ...row, image: githubAvatar(row.image) });
const historyPageSize = 50;
type HistoryCursor = { createdAt: number; id: string };
function encodeHistoryCursor(cursor: HistoryCursor) {
  return btoa(JSON.stringify(cursor)).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}
function decodeHistoryCursor(value: string | undefined): HistoryCursor | null {
  if (!value || value.length > 256 || !/^[A-Za-z0-9_-]+$/.test(value)) return null;
  try {
    const base64 = value.replaceAll("-", "+").replaceAll("_", "/");
    const parsed = JSON.parse(atob(base64 + "=".repeat((4 - base64.length % 4) % 4))) as Partial<HistoryCursor> | null;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    const createdAt = parsed.createdAt;
    if (typeof createdAt !== "number" || !Number.isSafeInteger(createdAt) || createdAt < 0 || typeof parsed.id !== "string" || !/^[A-Za-z0-9-]{1,100}$/.test(parsed.id)) return null;
    return { createdAt, id: parsed.id };
  } catch { return null; }
}
async function target(c: Ctx, handle: string) {
  return sql(c, "SELECT p.user_id AS id, p.handle FROM profiles p WHERE handle=? AND suspended=0", handle.replace(/^@/, "").toLowerCase()).first<{ id: string; handle: string }>();
}
async function notify(c: Ctx, ...ids: string[]) {
  await Promise.all(ids.map(id => c.env.HUBS.getByName(`user:${id}`).publish().catch(() => {
    console.warn(JSON.stringify({ event: "live_notification_failed" }));
  })));
}
async function rate(c: Ctx, key: string, limit: number, windowMs = 60_000) {
  const hash = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(key));
  const id = Array.from(new Uint8Array(hash), n => n.toString(16).padStart(2, "0")).join("");
  return c.env.HUBS.getByName(`rate:${id}`).consume("requests", limit, windowMs);
}

app.use("*", async (c, next) => {
  const url = new URL(c.req.url);
  if (url.origin !== c.env.BASE_URL) return error(c, "Unexpected host", 403);
  if (!isLocal(c.env) && (!c.env.BETTER_AUTH_SECRET || !c.env.TURNSTILE_SECRET || !c.env.TURNSTILE_SITE_KEY)) return error(c, "This server is not configured for public use yet", 503);
  if (c.req.method !== "GET" && c.req.method !== "HEAD") {
    const origin = c.req.header("Origin");
    if (origin && origin !== c.env.BASE_URL) return error(c, "Invalid request origin", 403);
    if (!c.req.header("Content-Type")?.startsWith("application/json")) return error(c, "JSON body required");
    if (!origin && c.req.header("X-Pokachy-Client") !== "cli") return error(c, "Client header required", 403);
    // Bound the cloned body before any authentication or JSON parsing.
    const reader = c.req.raw.clone().body?.getReader();
    if (reader) {
      let size = 0;
      for (;;) {
        const chunk = await reader.read();
        if (chunk.done) break;
        size += chunk.value.byteLength;
        if (size > 16_384) { void reader.cancel(); return error(c, "Request too large"); }
      }
    }
    const body = await c.req.raw.clone().json();
    if (body === null || typeof body !== "object" || Array.isArray(body)) return error(c, "A JSON object is required");
  }
  await next();
  if (c.res.status === 101) return;
  c.header("X-Content-Type-Options", "nosniff");
  c.header("Referrer-Policy", "no-referrer");
  c.header("X-Frame-Options", "DENY");
  c.header("Content-Security-Policy", "default-src 'self'; script-src 'self' https://challenges.cloudflare.com; frame-src https://challenges.cloudflare.com; connect-src 'self' https://challenges.cloudflare.com; style-src 'self'; img-src 'self' data: https://avatars.githubusercontent.com; base-uri 'none'; frame-ancestors 'none'; form-action 'self'");
  if (c.req.path.startsWith("/api/")) c.header("Cache-Control", "no-store");
  if (!isLocal(c.env)) c.header("Strict-Transport-Security", "max-age=31536000");
});

app.get("/api/config", c => c.json({ local: isLocal(c.env), github: !!(c.env.GITHUB_CLIENT_ID && c.env.GITHUB_CLIENT_SECRET), turnstileSiteKey: c.env.TURNSTILE_SITE_KEY, version: "0.1.9" }));
app.get("/health", async c => {
  c.header("Cache-Control", "no-store");
  try {
    await c.env.DB.prepare("SELECT 1").first();
    return c.json({ status: "ok", version: "0.1.9", revision: c.env.DEPLOY_REVISION ?? "unversioned" });
  } catch {
    return c.json({ status: "unavailable" }, 503);
  }
});

app.on(["GET", "POST"], "/api/auth/*", async c => {
  const ip = c.req.header("CF-Connecting-IP") ?? "local";
  if (!(await rate(c, `auth:${ip}`, 120))) return error(c, "Too many requests. Try again shortly.", 429);
  if (c.req.path.endsWith("/email-otp/send-verification-otp")) {
    const body = await c.req.raw.clone().json<{ email?: string }>();
    if (typeof body.email !== "string" || body.email.length > 254) return error(c, "Enter a valid email");
    if (!(await rate(c, `email:${body.email.toLowerCase()}`, 3, 300_000))) return error(c, "Please wait before requesting another code", 429);
    if (!isLocal(c.env)) {
      const token = c.req.header("X-Turnstile-Token");
      if (!token || token.length > 2048) return error(c, "Complete the browser verification", 403);
      try {
        const response = await fetch("https://challenges.cloudflare.com/turnstile/v0/siteverify", {
          method: "POST", signal: AbortSignal.timeout(10_000),
          body: new URLSearchParams({ secret: c.env.TURNSTILE_SECRET!, response: token, remoteip: ip }),
        });
        const result = await response.json<{ success: boolean; action: string; hostname: string }>();
        if (!response.ok || !result.success || result.action !== "login" || !c.env.TURNSTILE_HOSTNAMES.split(",").includes(result.hostname)) return error(c, "Verification failed. Please try again.", 403);
      } catch { return error(c, "Verification unavailable. Please try again.", 503); }
    }
  }
  return createAuth(c.env).handler(c.req.raw);
});

// Local-only email simulator. No real mail is sent during development.
app.get("/api/dev/mail", async c => {
  if (!isLocal(c.env)) return error(c, "Not found", 404);
  const row = await sql(c, "SELECT otp FROM dev_mail WHERE email=? AND expires_at>?", c.req.query("email") ?? "", Date.now()).first();
  return c.json(row ?? {});
});

app.route("/api/account", accountRoutes);

app.use("/api/*", async (c, next) => {
  const identity = await createAuth(c.env).api.getSession({ headers: c.req.raw.headers });
  if (!identity) return error(c, "Sign in to Pokachy", 401);
  if (!identity.user.emailVerified) return error(c, "Verify your email first", 403);
  const profile = await sql(c, "SELECT suspended FROM profiles WHERE user_id=?", identity.user.id).first<{ suspended: number }>();
  if (profile?.suspended) return error(c, "Your account is suspended", 403);
  c.set("identity", identity);
  if (c.req.method !== "GET" && !(await rate(c, `user:${identity.user.id}`, 60))) return error(c, "Slow down a little", 429);
  await next();
});

app.put("/api/profile", async c => {
  const body = await c.req.json<{ handle?: string; quiet?: boolean }>();
  if (body.handle !== undefined && typeof body.handle !== "string") return error(c, "Handle must be text");
  const existing = await sql(c, "SELECT handle FROM profiles WHERE user_id=?", uid(c)).first<{ handle: string }>();
  const handle = (body.handle ?? existing?.handle ?? "").replace(/^@/, "").toLowerCase();
  if (!handlePattern.test(handle)) return error(c, "Use 3–24 lowercase letters, numbers, or underscores");
  if (["admin", "support", "pokachy", "system"].includes(handle)) return error(c, "This handle is reserved");
  if (body.quiet !== undefined && typeof body.quiet !== "boolean") return error(c, "Quiet must be true or false");
  try {
    await sql(c, "INSERT INTO profiles(user_id,handle,quiet) VALUES (?,?,?) ON CONFLICT(user_id) DO UPDATE SET handle=excluded.handle, quiet=COALESCE(?,quiet)", uid(c), handle, body.quiet ? 1 : 0, body.quiet === undefined ? null : Number(body.quiet)).run();
  } catch (e) {
    if (String(e).includes("UNIQUE")) return error(c, "That handle is taken", 409);
    throw e;
  }
  await notify(c, uid(c));
  return c.json({ ok: true, handle });
});

app.get("/api/state", async c => {
  const id = uid(c);
  const results = await c.env.DB.batch<Record<string, unknown>>([
    sql(c, "SELECT handle,quiet FROM profiles WHERE user_id=?", id),
    sql(c, `SELECT p.handle,u.name,u.image,f.accepted,f.requester=? AS outgoing,
      EXISTS(SELECT 1 FROM pokes WHERE sender=? AND recipient=p.user_id AND resolved_at IS NULL) AS waiting
      FROM friendships f JOIN profiles p ON p.user_id=CASE WHEN f.a=? THEN f.b ELSE f.a END JOIN user u ON u.id=p.user_id
      WHERE (f.a=? OR f.b=?) AND p.suspended=0 ORDER BY f.accepted DESC,p.handle`, id,id,id,id,id),
    sql(c, "SELECT k.id,p.handle,u.name,u.image,k.created_at FROM pokes k JOIN profiles p ON p.user_id=k.sender JOIN user u ON u.id=k.sender WHERE k.recipient=? AND k.resolved_at IS NULL AND p.suspended=0 ORDER BY k.created_at DESC LIMIT 100", id),
    sql(c, "SELECT k.id,p.handle,u.name,u.image,k.created_at,k.sender=? AS outgoing,k.resolved_at FROM pokes k JOIN profiles p ON p.user_id=CASE WHEN k.sender=? THEN k.recipient ELSE k.sender END JOIN user u ON u.id=p.user_id WHERE k.sender=? OR k.recipient=? ORDER BY k.created_at DESC LIMIT 50",id,id,id,id),
    sql(c, "SELECT p.handle,u.name,u.image FROM blocks b JOIN profiles p ON p.user_id=b.blocked JOIN user u ON u.id=p.user_id WHERE b.blocker=?", id),
    sql(c, "SELECT COUNT(*) AS count FROM pokes WHERE recipient=?", id),
  ]);
  const profile = results[0].results[0];
  const friends = results[1].results.map(withAvatar);
  return c.json({ me: { id, name: c.get("identity").user.name, email: c.get("identity").user.email, image: githubAvatar(c.get("identity").user.image), handle: profile?.handle ?? null, quiet: !!profile?.quiet },
    friends: friends.filter(f => f.accepted), requests: friends.filter(f => !f.accepted),
    inbox: results[2].results.map(withAvatar), history: results[3].results.map(withAvatar), blocked: results[4].results.map(withAvatar),
    received: results[5].results[0]?.count ?? 0 });
});

app.get("/api/history/:handle", async c => {
  const other = await target(c, c.req.param("handle"));
  if (!other || other.id === uid(c)) return error(c, "Friend not found", 404);
  const [a, b] = pair(uid(c), other.id);
  const friendship = await sql(c, "SELECT 1 FROM friendships WHERE a=? AND b=? AND accepted=1", a, b).first();
  if (!friendship) return error(c, "Friend not found", 404);
  const rawCursor = c.req.query("before");
  const cursor = decodeHistoryCursor(rawCursor);
  if (rawCursor && !cursor) return error(c, "Invalid history cursor");
  const condition = cursor ? " AND (k.created_at < ? OR (k.created_at = ? AND k.id < ?))" : "";
  const args: unknown[] = [a, b, b, a];
  if (cursor) args.push(cursor.createdAt, cursor.createdAt, cursor.id);
  args.push(historyPageSize + 1);
  const rows = (await sql(c, `SELECT k.id,CASE WHEN k.sender=? THEN q.handle ELSE p.handle END AS handle,
      CASE WHEN k.sender=? THEN ru.name ELSE su.name END AS name,
      CASE WHEN k.sender=? THEN ru.image ELSE su.image END AS image,k.created_at,k.sender=? AS outgoing
      FROM pokes k JOIN profiles p ON p.user_id=k.sender JOIN profiles q ON q.user_id=k.recipient
      JOIN user su ON su.id=k.sender JOIN user ru ON ru.id=k.recipient
      WHERE ((k.sender=? AND k.recipient=?) OR (k.sender=? AND k.recipient=?))${condition}
      ORDER BY k.created_at DESC,k.id DESC LIMIT ?`, uid(c), uid(c), uid(c), uid(c), ...args).all()).results as Record<string, unknown>[];
  const history = rows.slice(0, historyPageSize).map(withAvatar);
  const last = history[history.length - 1];
  return c.json({ history, next_cursor: rows.length > historyPageSize && last ? encodeHistoryCursor({ createdAt: Number(last.created_at), id: String(last.id) }) : null });
});

app.use("/api/friends/*", async (c,next) => {
  if (!(await sql(c,"SELECT user_id FROM profiles WHERE user_id=?",uid(c)).first())) return error(c,"Choose a handle first",409);
  await next();
});
app.post("/api/friends/:handle", async c => {
  const other = await target(c,c.req.param("handle"));
  if (!other || other.id === uid(c)) return error(c,"No other user with that handle",404);
  const [a,b]=pair(uid(c),other.id);
  const blocked = await sql(c,"SELECT 1 FROM blocks WHERE (blocker=? AND blocked=?) OR (blocker=? AND blocked=?)",a,b,b,a).first();
  if (blocked) return error(c,"This friend request is unavailable",403);
  const result = await sql(c,`INSERT INTO friendships SELECT ?,?,?,0,?
    WHERE NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker=? AND blocked=?) OR (blocker=? AND blocked=?))
    ON CONFLICT(a,b) DO NOTHING`,a,b,uid(c),Date.now(),a,b,b,a).run();
  if (!result.meta.changes) return error(c,"You already have a friendship or pending request",409);
  await notify(c,other.id,uid(c));
  return c.json({ok:true});
});
app.post("/api/friends/:handle/accept", async c => {
  const other = await target(c,c.req.param("handle"));
  if (!other) return error(c,"Request not found",404);
  const [a,b]=pair(uid(c),other.id);
  const result = await sql(c,"UPDATE friendships SET accepted=1 WHERE a=? AND b=? AND requester=? AND accepted=0",a,b,other.id).run();
  if (!result.meta.changes) return error(c,"Incoming request not found",404);
  await notify(c,other.id,uid(c));
  return c.json({ok:true});
});
app.delete("/api/friends/:handle", async c => {
  const other = await target(c,c.req.param("handle"));
  if (!other) return error(c,"Friend not found",404);
  const [a,b]=pair(uid(c),other.id);
  await c.env.DB.batch([sql(c,"DELETE FROM friendships WHERE a=? AND b=?",a,b),sql(c,"UPDATE pokes SET resolved_at=? WHERE ((sender=? AND recipient=?) OR (sender=? AND recipient=?)) AND resolved_at IS NULL",Date.now(),a,b,b,a)]);
  await notify(c,a,b);
  return c.json({ok:true});
});

app.post("/api/pokes/:handle", async c => {
  const other=await target(c,c.req.param("handle"));
  if (!other || other.id===uid(c)) return error(c,"Friend not found",404);
  const sender=uid(c), recipient=other.id, now=Date.now();
  const key=c.req.header("Idempotency-Key");
  if (!key || !/^[a-zA-Z0-9-]{16,100}$/.test(key)) return error(c,"An Idempotency-Key is required");
  const old=await sql(c,"SELECT id,recipient FROM pokes WHERE sender=? AND request_key=?",sender,key).first<{id:string;recipient:string}>();
  if (old) return old.recipient===recipient ? c.json({ok:true,id:old.id}) : error(c,"Request key already used",409);
  const [a,b]=pair(sender,recipient);
  const id=crypto.randomUUID();
  const results=await c.env.DB.batch([
    sql(c,`INSERT INTO pokes(id,sender,recipient,created_at,request_key)
      SELECT ?,?,?,?,? WHERE EXISTS(SELECT 1 FROM friendships WHERE a=? AND b=? AND accepted=1)
      AND NOT EXISTS(SELECT 1 FROM blocks WHERE (blocker=? AND blocked=?) OR (blocker=? AND blocked=?))
      AND NOT EXISTS(SELECT 1 FROM pokes WHERE sender=? AND recipient=? AND (resolved_at IS NULL OR created_at>?))
      AND NOT EXISTS(SELECT 1 FROM profiles WHERE user_id IN (?,?) AND suspended=1)
      ON CONFLICT(sender,request_key) DO NOTHING`,id,sender,recipient,now,key,a,b,a,b,b,a,sender,recipient,now-10_000,sender,recipient),
    sql(c,"UPDATE pokes SET resolved_at=? WHERE sender=? AND recipient=? AND resolved_at IS NULL AND EXISTS(SELECT 1 FROM pokes WHERE id=?)",now,recipient,sender,id),
  ]);
  if (!results[0].meta.changes) {
    const retry=await sql(c,"SELECT id,recipient FROM pokes WHERE sender=? AND request_key=?",sender,key).first<{id:string;recipient:string}>();
    if(retry && retry.recipient===recipient) return c.json({ok:true,id:retry.id});
    return error(c,"Poke unavailable: accept the friendship, answer the pending poke, or wait 10 seconds",409);
  }
  await notify(c,sender,recipient);
  return c.json({ok:true,id},201);
});
app.post("/api/inbox/:id/dismiss",async c=>{
  const result=await sql(c,"UPDATE pokes SET resolved_at=? WHERE id=? AND recipient=? AND resolved_at IS NULL",Date.now(),c.req.param("id"),uid(c)).run();
  if(!result.meta.changes) return error(c,"Pending poke not found",404);
  const row=await sql(c,"SELECT sender FROM pokes WHERE id=?",c.req.param("id")).first<{sender:string}>();
  await notify(c,uid(c),row!.sender);
  return c.json({ok:true});
});
app.post("/api/blocks/:handle",async c=>{
  const other=await target(c,c.req.param("handle"));
  if(!other||other.id===uid(c)) return error(c,"User not found",404);
  const [a,b]=pair(uid(c),other.id);
  await c.env.DB.batch([
    sql(c,"INSERT INTO blocks VALUES (?,?) ON CONFLICT DO NOTHING",uid(c),other.id),
    sql(c,"DELETE FROM friendships WHERE a=? AND b=?",a,b),
    sql(c,"UPDATE pokes SET resolved_at=? WHERE ((sender=? AND recipient=?) OR (sender=? AND recipient=?)) AND resolved_at IS NULL",Date.now(),a,b,b,a),
  ]);
  await notify(c,a,b);
  return c.json({ok:true});
});
app.delete("/api/blocks/:handle",async c=>{
  await sql(c,"DELETE FROM blocks WHERE blocker=? AND blocked=(SELECT user_id FROM profiles WHERE handle=?)",uid(c),c.req.param("handle").replace(/^@/,"").toLowerCase()).run();
  return c.json({ok:true});
});
app.post("/api/reports/:handle",async c=>{
  const other=await target(c,c.req.param("handle"));
  const {reason}=await c.req.json<{reason:string}>();
  if(!other||other.id===uid(c)||typeof reason!=="string"||reason.trim().length<5||reason.length>500) return error(c,"Provide a user and a reason of 5–500 characters");
  if(!(await rate(c,`report:${uid(c)}`,5,3_600_000))) return error(c,"Report limit reached",429);
  await sql(c,"INSERT INTO reports(id,reporter,reported,reason,created_at) VALUES (?,?,?,?,?)",crypto.randomUUID(),uid(c),other.id,reason.trim(),Date.now()).run();
  return c.json({ok:true});
});
app.use("/api/admin/*",async(c,next)=>{
  if(!c.env.ADMIN_USER_IDS.split(",").filter(Boolean).includes(uid(c))) return error(c,"Administrator access required",403);
  await next();
});
app.get("/api/admin/reports",async c=>{
  const page=c.req.query("page");
  if (page===undefined) return c.json((await sql(c,"SELECT r.*,p.handle FROM reports r JOIN profiles p ON p.user_id=r.reported WHERE resolved=0 ORDER BY created_at DESC,id DESC LIMIT 100").all()).results);
  if (page!=="true") return error(c,"Invalid reports pagination request");
  const status=c.req.query("status")??"open";
  const resolved=status==="open" ? 0 : status==="resolved" ? 1 : status==="all" ? null : undefined;
  if (resolved===undefined) return error(c,"Invalid report status");
  const rawCursor=c.req.query("before");
  const cursor=decodeHistoryCursor(rawCursor);
  if(rawCursor&&!cursor) return error(c,"Invalid reports cursor");
  const conditions:string[]=[];
  const args:unknown[]=[];
  if(resolved!==null){conditions.push("r.resolved=?");args.push(resolved);}
  if(cursor){conditions.push("(r.created_at < ? OR (r.created_at = ? AND r.id < ?))");args.push(cursor.createdAt,cursor.createdAt,cursor.id);}
  args.push(51);
  const where=conditions.length ? `WHERE ${conditions.join(" AND ")}` : "";
  const rows=(await sql(c,`SELECT r.*,p.handle FROM reports r JOIN profiles p ON p.user_id=r.reported ${where} ORDER BY r.created_at DESC,r.id DESC LIMIT ?`,...args).all()).results as Record<string,unknown>[];
  const reports=rows.slice(0,50);
  const last=reports[reports.length-1];
  return c.json({reports,next_cursor:rows.length>50&&last?encodeHistoryCursor({createdAt:Number(last.created_at),id:String(last.id)}):null});
});
app.post("/api/admin/reports/:id/resolve",async c=>{
  const result=await sql(c,"UPDATE reports SET resolved=1 WHERE id=? AND resolved=0",c.req.param("id")).run();
  if(!result.meta.changes) return error(c,"Report not found",404);
  return c.json({ok:true});
});
app.post("/api/admin/reports/:id/reopen",async c=>{
  const result=await sql(c,"UPDATE reports SET resolved=0 WHERE id=? AND resolved=1",c.req.param("id")).run();
  if(!result.meta.changes) return error(c,"Report not found",404);
  return c.json({ok:true});
});
app.post("/api/admin/suspend/:handle",async c=>{
  const other=await target(c,c.req.param("handle"));
  if(!other || other.id===uid(c)) return error(c,"User not found",404);
  await c.env.DB.batch([sql(c,"UPDATE profiles SET suspended=1 WHERE user_id=?",other.id),sql(c,"DELETE FROM session WHERE userId=?",other.id)]);
  await notify(c,other.id);
  return c.json({ok:true});
});
app.post("/api/admin/unsuspend/:handle",async c=>{
  const handle=c.req.param("handle").replace(/^@/,"").toLowerCase();
  const other=await sql(c,"SELECT user_id AS id FROM profiles WHERE handle=?",handle).first<{id:string}>();
  if(!other || other.id===uid(c)) return error(c,"User not found",404);
  await sql(c,"UPDATE profiles SET suspended=0 WHERE user_id=? AND suspended=1",other.id).run();
  await notify(c,other.id);
  return c.json({ok:true});
});
app.get("/api/events",async c=>{
  const s=c.get("identity");
  const headers=new Headers(c.req.raw.headers);
  headers.set("X-User-ID",s.user.id);headers.set("X-Session-ID",s.session.id);headers.set("X-Session-Expiry",String(s.session.expiresAt.getTime()));
  return c.env.HUBS.getByName(`user:${s.user.id}`).fetch(new Request(c.req.raw,{headers}));
});
app.all("/api/*",c=>error(c,"Not found",404));
app.get("*",c=>{
  const url=new URL(c.req.url);
  if(["/activate","/account"].includes(url.pathname)) url.pathname="/index.html";
  return c.env.ASSETS.fetch(new Request(url,c.req.raw));
});
app.onError((err,c)=>{
  if(err instanceof SyntaxError) return error(c,"Invalid JSON");
  console.error(JSON.stringify({event:"request_failed",path:c.req.path,error:err.name}));
  return c.json({error:"Something went wrong. Please try again."},500);
});
export default {
  fetch: app.fetch,
  async scheduled(_controller: ScheduledController, env: AppEnv): Promise<void> {
    await cleanExpiredAuth(env);
  },
  async queue(batch: MessageBatch<MailJob>,env: AppEnv):Promise<void>{
    for(const message of batch.messages){
      const job=message.body;
      if(job.expiresAt<Date.now()){message.ack();continue;}
      try{
        if (!env.EMAIL) throw new Error("EMAIL is required");
        await env.EMAIL.send({from:env.MAIL_FROM,to:job.email,subject:"Your Pokachy sign-in code",text:`Your Pokachy code is ${job.otp}. It expires in 5 minutes. If you did not request it, ignore this email.`,html:`<p>Your Pokachy sign-in code:</p><h1>${job.otp}</h1><p>Expires in 5 minutes. If you did not request it, ignore this email.</p>`});
        message.ack();
      }catch{message.retry({delaySeconds:15});console.warn(JSON.stringify({event:"email_delivery_retry"}));}
    }
  },
} satisfies ExportedHandler<AppEnv,MailJob>;
