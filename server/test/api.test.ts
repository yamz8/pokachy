import { env } from "cloudflare:test";
import { beforeAll, beforeEach, expect, test, vi } from "vitest";
import worker from "../src/index";
import migration from "../migrations/0001_initial.sql?raw";
import profileSettingsMigration from "../migrations/0002_profile_settings.sql?raw";
import accountHandoffsMigration from "../migrations/0003_account_handoffs.sql?raw";
import { version } from "../package.json";

const origin = "http://127.0.0.1:8787";
const ctx = { waitUntil: () => {}, passThroughOnException: () => {} };
let testIP = 0;
beforeEach(() => { testIP++; });
async function request(path: string, token?: string, method="GET", body?: unknown, headers: Record<string,string>={}) {
  return worker.fetch(new Request(origin+path,{method,headers:{"Content-Type":"application/json","X-Pokachy-Client":"cli","CF-Connecting-IP":`192.0.2.${testIP}`,...(token?{Authorization:`Bearer ${token}`} : {}),...headers},body:body===undefined?undefined:JSON.stringify(body)}),env,ctx);
}
async function rawRequest(path: string, token: string, body: Uint8Array, contentType: string) {
  return worker.fetch(new Request(origin + path, {
    method: "PUT",
    headers: { "Content-Type": contentType, "X-Pokachy-Client": "cli", "CF-Connecting-IP": `192.0.2.${testIP}`, Authorization: `Bearer ${token}` },
    body,
  }), env, ctx);
}
async function user(handle: string) {
  const email=`${handle}@example.test`;
  const sent=await request("/api/auth/email-otp/send-verification-otp",undefined,"POST",{email,type:"sign-in"});
  expect(sent.status,await sent.clone().text()).toBe(200);
  const {otp}=await (await request(`/api/dev/mail?email=${email}`)).json() as {otp:string};
  const response=await request("/api/auth/sign-in/email-otp",undefined,"POST",{email,otp,name:handle});
  const data=await response.json() as {token:string;user:{id:string}};
  expect(response.status,JSON.stringify(data)).toBe(200);
  expect((await request("/api/profile",data.token,"PUT",{handle})).status).toBe(200);
  return data;
}
beforeAll(async()=>{ await env.DB.exec(migration); await env.DB.exec(profileSettingsMigration); await env.DB.exec(accountHandoffsMigration); });

test("friendly install route serves the getting-started page",async()=>{
  const response=await request("/install");
  expect(response.status).toBe(200);
  expect(await response.text()).toContain("Install Pokachy");
});

test("account handoffs are same-account, short-lived, single-use capabilities",async()=>{
  const owner=await user("handoffowner"), other=await user("handoffother");
  expect((await request("/api/account/handoff",owner.token,"POST",{action:"unsupported"})).status).toBe(400);
  const created=await request("/api/account/handoff",owner.token,"POST",{action:"email"});
  expect(created.status,await created.clone().text()).toBe(200);
  const data=await created.json() as {url:string;expiresIn:number};
  const target=new URL(data.url);
  const token=new URLSearchParams(target.hash.slice(1)).get("handoff")!;
  expect({origin:target.origin,path:target.pathname,query:target.search,action:new URLSearchParams(target.hash.slice(1)).get("action"),expiresIn:data.expiresIn})
    .toEqual({origin,path:"/account",query:"",action:"email",expiresIn:300});
  expect(token).toMatch(/^[0-9a-f]{64}$/);
  expect(await env.DB.prepare("SELECT 1 FROM account_handoffs WHERE token_hash=?").bind(token).first()).toBeNull();
  const digest=Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256",new TextEncoder().encode(token))),byte=>byte.toString(16).padStart(2,"0")).join("");
  expect(await env.DB.prepare("SELECT token_hash FROM account_handoffs").first()).toEqual({token_hash:digest});
  expect((await request("/api/account/handoff/claim",undefined,"POST",{token,action:"email"})).status).toBe(401);
  expect((await request("/api/account/handoff/claim",other.token,"POST",{token,action:"email"})).status).toBe(409);
  expect(await env.DB.prepare("SELECT 1 FROM account_handoffs").first()).not.toBeNull();
  expect((await request("/api/account/handoff/claim",owner.token,"POST",{token,action:"email"})).status).toBe(200);
  expect(await env.DB.prepare("SELECT 1 FROM account_handoffs").first()).toBeNull();
  expect((await request("/api/account/handoff/claim",owner.token,"POST",{token,action:"email"})).status).toBe(400);

  const expiring=await (await request("/api/account/handoff",owner.token,"POST",{action:"account"})).json() as {url:string};
  const expiredToken=new URLSearchParams(new URL(expiring.url).hash.slice(1)).get("handoff")!;
  await env.DB.prepare("UPDATE account_handoffs SET expires_at=?").bind(Date.now()-1).run();
  expect((await request("/api/account/handoff/claim",owner.token,"POST",{token:expiredToken,action:"account"})).status).toBe(400);
  expect(await env.DB.prepare("SELECT 1 FROM account_handoffs").first()).toBeNull();
});

test("email onboarding, consent, idempotency, concurrency, reply, block and isolation",async()=>{
  const a=await user("alice"), b=await user("bobby"), x=await user("outsider");
  const poke=(token:string,handle:string,key=crypto.randomUUID())=>request(`/api/pokes/${handle}`,token,"POST",{}, {"Idempotency-Key":key});
  expect((await request("/api/state")).status).toBe(401);
  expect((await poke(a.token,"bobby")).status).toBe(409);
  expect((await request("/api/friends/bobby",a.token,"POST",{})).status).toBe(200);
  expect((await request("/api/friends/bobby/accept",a.token,"POST",{})).status).toBe(404);
  expect((await request("/api/friends/alice/accept",b.token,"POST",{})).status).toBe(200);
  const key=crypto.randomUUID();
  const results=await Promise.all([poke(a.token,"bobby",key),poke(a.token,"bobby",key)]);
  expect(results.map(r=>r.status).sort()).toEqual([200,201]);
  expect((await poke(a.token,"bobby")).status).toBe(409);
  let state=await (await request("/api/state",b.token)).json();
  expect(state.inbox).toHaveLength(1);
  expect(JSON.stringify(state.friends)).not.toContain("example.test");
  expect((await request(`/api/inbox/${state.inbox[0].id}/dismiss`,x.token,"POST",{})).status).toBe(404);
  expect((await poke(b.token,"alice")).status).toBe(201);
  state=await (await request("/api/state",b.token)).json();
  expect(state.inbox).toHaveLength(0);
  expect((await request("/api/profile",a.token,"PUT",{quiet:true})).status).toBe(200);
  const quiet=await (await request("/api/state",a.token)).json();
  expect(quiet.me.quiet).toBe(true); expect(quiet.inbox).toHaveLength(1);
  expect((await request("/api/blocks/bobby",a.token,"POST",{})).status).toBe(200);
  expect((await request("/api/friends/alice",b.token,"POST",{})).status).toBe(403);
  expect((await poke(b.token,"alice")).status).toBe(409);
  expect((await request("/api/admin/reports",a.token)).status).toBe(403);
  expect((await request("/api/state",a.token)).status).toBe(200);
  const spoof=await request("/api/profile",a.token,"PUT",{quiet:false},{Origin:"https://attacker.test"});
  expect(spoof.status).toBe(403);
  await request("/api/auth/sign-out",a.token,"POST",{});
  expect((await request("/api/state",a.token)).status).toBe(401);
});

test("administrators can review reports and suspend another user",async()=>{
  const admin=await user("adminuser"),reported=await user("reporteduser"),reporter=await user("reporteruser");
  expect((await request("/api/reports/reporteduser",reporter.token,"POST",{reason:"Authorized abuse report test"})).status).toBe(200);
  const adminEnv={...env,ADMIN_USER_IDS:admin.user.id} as typeof env;
  const adminRequest=(path:string,method="GET")=>worker.fetch(new Request(origin+path,{
    method,headers:{"Content-Type":"application/json","X-Pokachy-Client":"cli","CF-Connecting-IP":`192.0.2.${testIP}`,Authorization:`Bearer ${admin.token}`},
    body:method==="GET"?undefined:"{}",
  }),adminEnv,ctx);
  const reports=await adminRequest("/api/admin/reports");
  expect(reports.status).toBe(200);
  expect(await reports.json()).toEqual(expect.arrayContaining([expect.objectContaining({handle:"reporteduser",reason:"Authorized abuse report test"})]));
  expect((await adminRequest("/api/admin/suspend/adminuser","POST")).status).toBe(404);
  expect((await adminRequest("/api/admin/suspend/reporteduser","POST")).status).toBe(200);
  expect((await request("/api/state",reported.token)).status).toBe(401);
});

test("history is friend-scoped and cursor-paginated",async()=>{
  const a=await user("historyalice"), b=await user("historybobby"), outsider=await user("historyoutsider");
  const image="https://avatars.githubusercontent.com/u/9919?v=4";
  await env.DB.prepare("UPDATE user SET image=? WHERE id=?").bind(image,b.user.id).run();
  expect((await request("/api/friends/historybobby",a.token,"POST",{})).status).toBe(200);
  expect((await request("/api/friends/historyalice/accept",b.token,"POST",{})).status).toBe(200);
  for(let i=0;i<52;i++) {
    await env.DB.prepare("INSERT INTO pokes(id,sender,recipient,created_at,resolved_at,request_key) VALUES (?,?,?,?,?,?)")
      .bind(crypto.randomUUID(), i%2 ? b.user.id : a.user.id, i%2 ? a.user.id : b.user.id, 2_000_000+Math.floor(i/3), 2_000_001+i, crypto.randomUUID()).run();
  }
  const first=await request("/api/history/historybobby",a.token);
  expect(first.status).toBe(200);
  const page=await first.json() as {history: {id:string;handle:string;image:string|null;created_at:number;outgoing:number}[];next_cursor:string|null};
  expect(page.history).toHaveLength(50); expect(page.next_cursor).toEqual(expect.any(String));
  const second=await request(`/api/history/historybobby?before=${encodeURIComponent(page.next_cursor!)}`,a.token);
  expect(second.status).toBe(200);
  const older=await second.json() as typeof page;
  expect(older.history).toHaveLength(2); expect(older.next_cursor).toBeNull();
  const all=page.history.concat(older.history);
  expect(new Set(all.map(p=>p.id)).size).toBe(52);
  expect(all.every(p=>p.handle==="historybobby")).toBe(true);
  expect(all.every(p=>p.image===image)).toBe(true);
  expect(all.filter(p=>p.outgoing===1)).toHaveLength(26);
  expect(all.map(p=>p.created_at)).toEqual(all.map(p=>p.created_at).sort((a,b)=>b-a));
  expect((await request("/api/history/historybobby?before=bad",a.token)).status).toBe(400);
  expect((await request("/api/history/historybobby",outsider.token)).status).toBe(404);
  expect((await request("/api/history/historybobby")).status).toBe(401);
  expect((await request("/api/history/historybobby?before="+"x".repeat(257),a.token)).status).toBe(400);
});

test("device onboarding requires explicit approval and a one-time redemption",async()=>{
  const u=await user("deviceuser");
  const started=await request("/api/auth/device/code",undefined,"POST",{client_id:"pokachy-cli"});
  const codes=await started.json(); expect(started.status,JSON.stringify(codes)).toBe(200);
  const invalid=await request("/api/auth/device/code",undefined,"POST",{client_id:"unknown-client"});
  expect(invalid.ok).toBe(false);
  const unclaimed=await request("/api/auth/device/approve",u.token,"POST",{userCode:codes.user_code});
  expect(unclaimed.ok).toBe(false);
  expect((await request(`/api/auth/device?user_code=${codes.user_code}`,u.token)).status).toBe(200);
  const approved=await request("/api/auth/device/approve",u.token,"POST",{userCode:codes.user_code});
  expect(approved.status,await approved.clone().text()).toBe(200);
  const response=await request("/api/auth/device/token",undefined,"POST",{client_id:"pokachy-cli",device_code:codes.device_code,grant_type:"urn:ietf:params:oauth:grant-type:device_code"});
  const auth=await response.json();expect(response.status,JSON.stringify(auth)).toBe(200);
  expect((await request("/api/state",auth.access_token)).status).toBe(200);
  const replay=await request("/api/auth/device/token",undefined,"POST",{client_id:"pokachy-cli",device_code:codes.device_code,grant_type:"urn:ietf:params:oauth:grant-type:device_code"});
  expect(replay.ok).toBe(false);
});

test("bad and replayed email codes fail; handles are unique",async()=>{
  const u=await user("uniqueuser");
  const v=await user("seconduser");
  expect((await request("/api/profile",v.token,"PUT",{handle:"uniqueuser"})).status).toBe(409);
  const failed=await request("/api/auth/sign-in/email-otp",undefined,"POST",{email:"uniqueuser@example.test",otp:"000000"});
  expect(failed.ok).toBe(false);
  expect((await request("/api/profile",u.token,"PUT",{handle:"bad<script>"})).status).toBe(400);
  const used=await (await request("/api/dev/mail?email=uniqueuser@example.test")).json();
  expect((await request("/api/auth/sign-in/email-otp",undefined,"POST",{email:"uniqueuser@example.test",otp:used.otp})).ok).toBe(false);
  expect((await request("/api/profile",u.token,"PUT",null)).status).toBe(400);
});

test("state exposes GitHub pictures but rejects untrusted external images",async()=>{
  const a=await user("avataralice"),b=await user("avatarbobby");
  const githubImage="https://avatars.githubusercontent.com/u/123456?v=4";
  await env.DB.batch([
    env.DB.prepare("UPDATE user SET image=? WHERE id=?").bind(githubImage,a.user.id),
    env.DB.prepare("UPDATE user SET image=? WHERE id=?").bind("https://tracker.example/avatar.png",b.user.id),
  ]);
  await request("/api/friends/avatarbobby",a.token,"POST",{});
  expect((await (await request("/api/state",b.token)).json()).requests[0].image).toBe(githubImage);
  await request("/api/friends/avataralice/accept",b.token,"POST",{});
  expect((await request("/api/pokes/avatarbobby",a.token,"POST",{}, {"Idempotency-Key":crypto.randomUUID()})).status).toBe(201);
  const alice=await (await request("/api/state",a.token)).json();
  const bobby=await (await request("/api/state",b.token)).json();
  expect(alice.me.image).toBe(githubImage);
  expect(alice.friends[0].image).toBeNull();
  expect(alice.history[0].image).toBeNull();
  expect(bobby.me.image).toBeNull();
  expect(bobby.friends[0].image).toBe(githubImage);
  expect(bobby.inbox[0].image).toBe(githubImage);
  expect((await (await request("/api/history/avataralice",b.token)).json()).history[0].image).toBe(githubImage);
  expect((await (await request("/api/history/avatarbobby",a.token)).json()).history[0].image).toBeNull();
  for(const rejected of [null,"not a URL","http://avatars.githubusercontent.com/u/1","https://avatars.githubusercontent.com.evil.test/u/1","https://user@avatars.githubusercontent.com/u/1","https://avatars.githubusercontent.com:444/u/1"]) {
    await env.DB.prepare("UPDATE user SET image=? WHERE id=?").bind(rejected,a.user.id).run();
    expect((await (await request("/api/state",b.token)).json()).friends[0].image).toBeNull();
  }
});

test("state reports only the authenticated user's GitHub connection",async()=>{
  const owner=await user("githubowner"), other=await user("githubother");
  let state=await (await request("/api/state",owner.token)).json() as {me:{githubAvailable:boolean;githubLinked:boolean}};
  expect(state.me).toEqual(expect.objectContaining({githubAvailable:false,githubLinked:false}));
  const now=Date.now();
  await env.DB.prepare("INSERT INTO account(id,accountId,providerId,userId,createdAt,updatedAt) VALUES (?,?,?,?,?,?)")
    .bind(crypto.randomUUID(),"other-github","github",other.user.id,now,now).run();
  state=await (await request("/api/state",owner.token)).json() as typeof state;
  expect(state.me.githubLinked).toBe(false);
  await env.DB.prepare("INSERT INTO account(id,accountId,providerId,userId,createdAt,updatedAt) VALUES (?,?,?,?,?,?)")
    .bind(crypto.randomUUID(),"owner-github","github",owner.user.id,now,now).run();
  state=await (await request("/api/state",owner.token)).json() as typeof state;
  expect(state.me.githubLinked).toBe(true);
});

test("email changes require codes from both the current and new inboxes",async()=>{
  const owner=await user("emailchangeowner");
  const currentEmail="emailchangeowner@example.test", newEmail="emailchangeowner-new@example.test";
  const started=await request("/api/account/email-change/current-code",owner.token,"POST",{});
  expect(started.status,await started.clone().text()).toBe(200);
  const currentCode=(await (await request(`/api/dev/mail?email=${currentEmail}`)).json() as {otp:string}).otp;

  expect((await request("/api/auth/email-otp/request-email-change",owner.token,"POST",{newEmail,otp:"000000"})).status).toBe(400);
  const requested=await request("/api/auth/email-otp/request-email-change",owner.token,"POST",{newEmail,otp:currentCode});
  expect(requested.status,await requested.clone().text()).toBe(200);
  expect((await (await request("/api/state",owner.token)).json()).me.email).toBe(currentEmail);
  const newCode=(await (await request(`/api/dev/mail?email=${newEmail}`)).json() as {otp:string}).otp;

  expect((await request("/api/auth/email-otp/change-email",owner.token,"POST",{newEmail,otp:"000000"})).status).toBe(400);
  const changed=await request("/api/auth/email-otp/change-email",owner.token,"POST",{newEmail,otp:newCode});
  expect(changed.status,await changed.clone().text()).toBe(200);
  expect((await (await request("/api/state",owner.token)).json()).me.email).toBe(newEmail);
  expect(await env.DB.prepare("SELECT 1 FROM user WHERE id=? AND email=? AND emailVerified=1").bind(owner.user.id,newEmail).first()).not.toBeNull();
});

test("profile settings update display identity and safely replace uploaded avatars",async()=>{
  const owner=await user("settingsowner"),friend=await user("settingsfriend");
  const githubImage="https://avatars.githubusercontent.com/u/42?v=4";
  await env.DB.prepare("UPDATE user SET image=? WHERE id=?").bind(githubImage,owner.user.id).run();
  await request("/api/friends/settingsfriend",owner.token,"POST",{});
  await request("/api/friends/settingsowner/accept",friend.token,"POST",{});

  const updated=await request("/api/profile",owner.token,"PUT",{handle:"newhandle",name:"New Name",userId:friend.user.id});
  expect(updated.status,await updated.clone().text()).toBe(200);
  let friendState=await (await request("/api/state",friend.token)).json() as {me:{handle:string;name:string};friends:Array<{handle:string;name:string;image:string|null}>};
  expect(friendState.me).toEqual(expect.objectContaining({handle:"settingsfriend",name:"settingsfriend"}));
  expect(friendState.friends[0]).toEqual(expect.objectContaining({handle:"newhandle",name:"New Name",image:githubImage}));
  expect((await request("/api/profile",owner.token,"PUT",{name:"\u202ehidden"})).status).toBe(400);

  const png=new Uint8Array([0x89,0x50,0x4e,0x47,0x0d,0x0a,0x1a,0x0a,1,2,3]);
  const uploaded=await rawRequest("/api/profile/image",owner.token,png,"image/png");
  expect(uploaded.status,await uploaded.clone().text()).toBe(200);
  const firstKey=(await env.DB.prepare("SELECT avatar_key FROM profiles WHERE user_id=?").bind(owner.user.id).first<{avatar_key:string}>())!.avatar_key;
  friendState=await (await request("/api/state",friend.token)).json() as typeof friendState;
  const customURL=friendState.friends[0].image!;
  expect(customURL).toBe(`${origin}/avatars/${owner.user.id}/${firstKey.split("/")[2]}`);
  const image=await request(new URL(customURL).pathname);
  expect(image.status).toBe(200);
  expect(image.headers.get("Content-Type")).toBe("image/png");
  expect(new Uint8Array(await image.arrayBuffer())).toEqual(png);

  expect((await rawRequest("/api/profile/image",owner.token,new Uint8Array([1,2,3]),"image/png")).status).toBe(400);
  expect((await rawRequest("/api/profile/image",owner.token,new Uint8Array(2*1024*1024+1),"image/png")).status).toBe(400);
  expect(await env.AVATARS.get(firstKey)).not.toBeNull();
  const jpeg=new Uint8Array([0xff,0xd8,0xff,0xd9]);
  expect((await rawRequest("/api/profile/image",owner.token,jpeg,"image/jpeg")).status).toBe(200);
  const secondKey=(await env.DB.prepare("SELECT avatar_key FROM profiles WHERE user_id=?").bind(owner.user.id).first<{avatar_key:string}>())!.avatar_key;
  expect(secondKey).not.toBe(firstKey);
  expect(await env.AVATARS.get(firstKey)).toBeNull();
  expect(await env.AVATARS.get(secondKey)).not.toBeNull();
  expect((await request("/api/profile/image",owner.token,"DELETE",{})).status).toBe(200);
  expect(await env.AVATARS.get(secondKey)).toBeNull();
  const fallback=await (await request("/api/state",friend.token)).json() as typeof friendState;
  expect(fallback.friends[0].image).toBe(githubImage);
  expect((await request(new URL(customURL).pathname)).status).toBe(404);
});

test("live updates stay private and a revoked session loses its connection",async()=>{
  const a=await user("livealice"),b=await user("livebobby");
  await request("/api/friends/livebobby",a.token,"POST",{});
  await request("/api/friends/livealice/accept",b.token,"POST",{});
  const response=await request("/api/events",b.token,"GET",undefined,{Upgrade:"websocket"});
  expect(response.status).toBe(101);
  const ws=response.webSocket!;
  const events:string[]=[];
  let closed=false;
  ws.addEventListener("message",event=>events.push(String(event.data)));
  ws.addEventListener("close",()=>{closed=true;});
  ws.accept();
  await expect.poll(()=>events.length).toBe(1);
  const sent=await request("/api/pokes/livebobby",a.token,"POST",{},{"Idempotency-Key":crypto.randomUUID()});
  expect(sent.status).toBe(201);
  await expect.poll(()=>events.length).toBe(2);
  expect(events.every(value=>value==='{"type":"sync"}')).toBe(true);
  ws.send("ping");
  await expect.poll(()=>events.includes("pong")).toBe(true);
  await request("/api/auth/sign-out",b.token,"POST",{});
  await request("/api/friends/livebobby",a.token,"DELETE",{});
  await expect.poll(()=>closed).toBe(true);
});

test("public OTP requests require a valid Turnstile action and hostname",async()=>{
  const production={...env,ENVIRONMENT:"production",BASE_URL:"https://pokachy.com",TURNSTILE_SECRET:"test-secret",TURNSTILE_SITE_KEY:"test-sitekey",TURNSTILE_HOSTNAMES:"pokachy.com",BETTER_AUTH_SECRET:"production-test-auth-secret-not-for-live-use"};
  const send=(token?:string)=>worker.fetch(new Request("https://pokachy.com/api/auth/email-otp/send-verification-otp",{
    method:"POST",headers:{"Content-Type":"application/json",Origin:"https://pokachy.com","CF-Connecting-IP":"192.0.2.99",...(token?{"X-Turnstile-Token":token}:{})},body:JSON.stringify({email:"captcha@example.test",type:"sign-in"})}),production,ctx);
  expect((await send()).status).toBe(403);
  const mocked=vi.spyOn(globalThis,"fetch").mockResolvedValue(new Response(JSON.stringify({success:true,action:"login",hostname:"attacker.test"})));
  try { expect((await send("fake-token")).status).toBe(403); }
  finally { mocked.mockRestore(); }
  const probe=await worker.fetch(new Request("https://pokachy.com/api/dev/mail?email=captcha@example.test"),production,ctx);
  expect(probe.status).toBe(404);
});

test("mail consumer retries failures and discards expired codes",async()=>{
  const send=vi.fn().mockRejectedValue(new Error("temporary"));
  const fresh={body:{email:"queue@example.test",otp:"123456",expiresAt:Date.now()+60_000},ack:vi.fn(),retry:vi.fn()};
  const expired={body:{email:"queue@example.test",otp:"654321",expiresAt:Date.now()-1},ack:vi.fn(),retry:vi.fn()};
  await worker.queue({messages:[fresh,expired]} as never,{...env,EMAIL:{send}} as never);
  expect(send).toHaveBeenCalledOnce();expect(fresh.retry).toHaveBeenCalledOnce();expect(expired.ack).toHaveBeenCalledOnce();
});

test("public config fails closed without production secrets",async()=>{
  const production={...env,ENVIRONMENT:"production",BASE_URL:"https://pokachy.com"};
  const res=await worker.fetch(new Request("https://pokachy.com/api/dev/mail?email=a@example.test"),production,ctx);
  expect(res.status).toBe(503);
});


test("health reports deployment identity and fails closed on database outage", async () => {
  const response = await worker.fetch(new Request(origin + "/health"), { ...env, DEPLOY_REVISION: "test-revision" }, ctx);
  expect(response.status).toBe(200);
  expect(response.headers.get("Cache-Control")).toBe("no-store");
  expect(await response.json()).toEqual({ status: "ok", version, revision: "test-revision" });
  const prepare = vi.spyOn(env.DB, "prepare").mockImplementation(() => { throw new Error("private database detail"); });
  try {
    const failed = await request("/health");
    expect(failed.status).toBe(503);
    expect(await failed.json()).toEqual({ status: "unavailable" });
  } finally { prepare.mockRestore(); }
});
