import { env } from "cloudflare:test";
import { beforeAll, expect, test } from "vitest";
import worker from "../src/index";
import migration from "../migrations/0001_initial.sql?raw";
import profileSettingsMigration from "../migrations/0002_profile_settings.sql?raw";
import accountHandoffsMigration from "../migrations/0003_account_handoffs.sql?raw";

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
  return data;
}

beforeAll(async () => { await env.DB.exec(migration); await env.DB.exec(profileSettingsMigration); await env.DB.exec(accountHandoffsMigration); });

test("administrators paginate and resolve reports, manage suspension, and unblock case-insensitively", async () => {
  const admin = await user("moderatoradmin");
  const reporter = await user("moderatorreporter");
  const reported = await user("moderationtarget");
  const adminRequest = (path: string, method = "GET", body?: unknown) => request(path, admin.token, method, body, admin.user.id);

  expect((await request("/api/admin/reports", reporter.token)).status).toBe(403);
  for (let number = 1; number <= 51; number++) {
    const id = `00000000-0000-4000-8000-${String(number).padStart(12, "0")}`;
    await env.DB.prepare("INSERT INTO reports(id,reporter,reported,reason,created_at) VALUES (?,?,?,?,?)")
      .bind(id, reporter.user.id, reported.user.id, `Test report ${number}`, 1000).run();
  }

  const legacy = await adminRequest("/api/admin/reports");
  expect(legacy.status).toBe(200);
  expect(Array.isArray(await legacy.json())).toBe(true);
  expect((await adminRequest("/api/admin/reports?page=yes")).status).toBe(400);
  expect((await adminRequest("/api/admin/reports?page=true&status=closed")).status).toBe(400);
  expect((await adminRequest("/api/admin/reports?page=true&before=invalid")).status).toBe(400);

  const first = await adminRequest("/api/admin/reports?page=true");
  const page = await first.json() as { reports: { id: string }[]; next_cursor: string | null };
  expect(first.status).toBe(200);
  expect(page.reports).toHaveLength(50);
  expect(page.next_cursor).toEqual(expect.any(String));
  expect(page.reports.map(report => report.id)).toEqual([...page.reports.map(report => report.id)].sort().reverse());
  const second = await adminRequest(`/api/admin/reports?page=true&before=${encodeURIComponent(page.next_cursor!)}`);
  const next = await second.json() as typeof page;
  expect(second.status).toBe(200);
  expect(next.reports).toHaveLength(1);
  expect(new Set(page.reports.concat(next.reports).map(report => report.id)).size).toBe(51);

  const reportId = page.reports[0].id;
  expect((await adminRequest(`/api/admin/reports/${reportId}/resolve`, "POST", {})).status).toBe(200);
  expect((await adminRequest(`/api/admin/reports/${reportId}/resolve`, "POST", {})).status).toBe(404);
  const resolved = await adminRequest("/api/admin/reports?page=true&status=resolved");
  expect((await resolved.json() as { reports: { id: string }[] }).reports.map(report => report.id)).toContain(reportId);
  expect((await adminRequest(`/api/admin/reports/${reportId}/reopen`, "POST", {})).status).toBe(200);

  expect((await request("/api/blocks/moderationtarget", reporter.token, "POST", {})).status).toBe(200);
  expect((await request("/api/blocks/@MODERATIONTARGET", reporter.token, "DELETE", {})).status).toBe(200);
  expect((await env.DB.prepare("SELECT 1 FROM blocks WHERE blocker=? AND blocked=?").bind(reporter.user.id, reported.user.id).first())).toBeNull();

  expect((await adminRequest("/api/admin/suspend/moderatoradmin", "POST", {})).status).toBe(404);
  expect((await adminRequest("/api/admin/suspend/moderationtarget", "POST", {})).status).toBe(200);
  expect((await request("/api/state", reported.token)).status).toBe(401);
  expect((await adminRequest("/api/admin/unsuspend/moderatoradmin", "POST", {})).status).toBe(404);
  expect((await adminRequest("/api/admin/unsuspend/@MODERATIONTARGET", "POST", {})).status).toBe(200);
  expect((await env.DB.prepare("SELECT suspended FROM profiles WHERE user_id=?").bind(reported.user.id).first<{ suspended: number }>())).toEqual({ suspended: 0 });
  expect((await env.DB.prepare("SELECT COUNT(*) AS count FROM session WHERE userId=?").bind(reported.user.id).first<{ count: number }>())).toEqual({ count: 0 });
  expect((await request("/api/state", reported.token)).status).toBe(401);
});
