import { sqliteTable, text, integer } from "drizzle-orm/sqlite-core";

const date = (name: string) => integer(name, { mode: "timestamp_ms" });
export const user = sqliteTable("user", {
  id: text("id").primaryKey(), name: text("name").notNull(), email: text("email").notNull().unique(),
  emailVerified: integer("emailVerified", { mode: "boolean" }).notNull(), image: text("image"),
  createdAt: date("createdAt").notNull(), updatedAt: date("updatedAt").notNull(),
});
export const session = sqliteTable("session", {
  id: text("id").primaryKey(), token: text("token").notNull().unique(),
  expiresAt: date("expiresAt").notNull(), createdAt: date("createdAt").notNull(), updatedAt: date("updatedAt").notNull(),
  ipAddress: text("ipAddress"), userAgent: text("userAgent"),
  userId: text("userId").notNull().references(() => user.id, { onDelete: "cascade" }),
});
export const account = sqliteTable("account", {
  id: text("id").primaryKey(), accountId: text("accountId").notNull(), providerId: text("providerId").notNull(),
  userId: text("userId").notNull().references(() => user.id, { onDelete: "cascade" }),
  accessToken: text("accessToken"), refreshToken: text("refreshToken"), idToken: text("idToken"),
  accessTokenExpiresAt: date("accessTokenExpiresAt"), refreshTokenExpiresAt: date("refreshTokenExpiresAt"),
  scope: text("scope"), password: text("password"), createdAt: date("createdAt").notNull(), updatedAt: date("updatedAt").notNull(),
});
export const verification = sqliteTable("verification", {
  id: text("id").primaryKey(), identifier: text("identifier").notNull(), value: text("value").notNull(),
  expiresAt: date("expiresAt").notNull(), createdAt: date("createdAt").notNull(), updatedAt: date("updatedAt").notNull(),
});
export const deviceCode = sqliteTable("deviceCode", {
  id: text("id").primaryKey(), deviceCode: text("deviceCode").notNull().unique(), userCode: text("userCode").notNull().unique(),
  userId: text("userId"), expiresAt: date("expiresAt").notNull(), status: text("status").notNull(),
  lastPolledAt: date("lastPolledAt"), pollingInterval: integer("pollingInterval"), clientId: text("clientId"), scope: text("scope"),
});
export const rateLimit = sqliteTable("rateLimit", {
  id: text("id").primaryKey(), key: text("key").notNull().unique(), count: integer("count").notNull(), lastRequest: integer("lastRequest").notNull(),
});
