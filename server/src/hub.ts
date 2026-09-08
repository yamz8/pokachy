import { DurableObject } from "cloudflare:workers";
import type { AppEnv } from "./env";

type Attachment = { userId: string; sessionId: string; expiresAt: number };
export class UserHub extends DurableObject<AppEnv> {
  constructor(ctx: DurableObjectState, env: AppEnv) {
    super(ctx, env);
    ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS budgets (key TEXT PRIMARY KEY, count INTEGER NOT NULL, resets INTEGER NOT NULL)");
    ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair("ping", "pong"));
  }
  async consume(key: string, limit: number, windowMs: number): Promise<boolean> {
    const now = Date.now();
    this.ctx.storage.sql.exec("DELETE FROM budgets WHERE resets <= ?", now);
    const row = this.ctx.storage.sql.exec<{ count: number }>("INSERT INTO budgets VALUES (?,1,?) ON CONFLICT(key) DO UPDATE SET count=count+1 RETURNING count", key, now + windowMs).one();
    return row.count <= limit;
  }
  async fetch(request: Request): Promise<Response> {
    if (request.headers.get("Upgrade")?.toLowerCase() !== "websocket") return new Response("WebSocket required", { status: 426 });
    if (this.ctx.getWebSockets().length >= 8) return new Response("Too many connections", { status: 429 });
    const attachment: Attachment = { userId: request.headers.get("X-User-ID")!, sessionId: request.headers.get("X-Session-ID")!, expiresAt: Number(request.headers.get("X-Session-Expiry")) };
    const pair = new WebSocketPair();
    this.ctx.acceptWebSocket(pair[1]);
    pair[1].serializeAttachment(attachment);
    pair[1].send(JSON.stringify({ type: "sync" }));
    return new Response(null, { status: 101, webSocket: pair[0] });
  }
  async publish(): Promise<void> {
    for (const socket of this.ctx.getWebSockets()) {
      const a = socket.deserializeAttachment() as Attachment;
      const valid = a && a.expiresAt > Date.now() && await this.env.DB.prepare("SELECT s.id FROM session s JOIN profiles p ON p.user_id=s.userId WHERE s.id=? AND s.userId=? AND s.expiresAt>? AND p.suspended=0").bind(a.sessionId, a.userId, Date.now()).first();
      if (!valid) { socket.close(1008, "Session expired"); continue; }
      try { socket.send(JSON.stringify({ type: "sync" })); } catch { socket.close(1011, "Reconnect"); }
    }
  }
  webSocketMessage(socket: WebSocket): void { socket.close(1008, "Use the HTTP API for commands"); }
  webSocketClose(socket: WebSocket): void { socket.close(); }
}
