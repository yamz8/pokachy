import { env } from "cloudflare:test";
import { beforeAll, beforeEach, expect, test, vi } from "vitest";
import worker from "../src/index";
import migration from "../migrations/0001_initial.sql?raw";

const origin = "http://127.0.0.1:8787";
const ctx = { waitUntil: () => {}, passThroughOnException: () => {} };
let testIP = 0;
beforeEach(() => { testIP++; });
async function request(path: string, token?: string, method="GET", body?: unknown, headers: Record<string,string>={}) {
  return worker.fetch(new Request(origin+path,{method,headers:{"Content-Type":"application/json","X-Pokachy-Client":"cli","CF-Connecting-IP":`192.0.2.${testIP}`,...(token?{Authorization:`Bearer ${token}`} : {}),...headers},body:body===undefined?undefined:JSON.stringify(body)}),env,ctx);
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
beforeAll(async()=>{ await env.DB.exec(migration); });

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
