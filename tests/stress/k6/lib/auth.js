// Mint a local-dev HS256 JWT — mirrors core-api's scripts/gentoken.go.
// The server (CLERK_JWKS_URL unset) validates it with JWT_SECRET and reads the
// `sub` claim as the user ID. EnsureUser then upserts the user + grants credits
// on the first authed request, so no Clerk and no seeding are required.

import crypto from "k6/crypto";
import encoding from "k6/encoding";
import { JWT_SECRET } from "./config.js";

function b64url(obj) {
  return encoding.b64encode(JSON.stringify(obj), "rawurl");
}

export function mintJWT(userID, ttlSeconds = 3600) {
  const now = Math.floor(Date.now() / 1000);
  const header = b64url({ alg: "HS256", typ: "JWT" });
  const payload = b64url({ sub: userID, iat: now, exp: now + ttlSeconds });
  const signingInput = `${header}.${payload}`;
  const sig = crypto.hmac("sha256", JWT_SECRET, signingInput, "base64rawurl");
  return `${signingInput}.${sig}`;
}
