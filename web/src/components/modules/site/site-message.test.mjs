import assert from "node:assert/strict";
import test from "node:test";

import { translateSiteMessage } from "./site-message.ts";

const noAvailableKeyMessage =
  "site sync requires at least one available key; create a key for an existing group on the site and sync again";

test("translates no-key sync errors without inventing a default group", () => {
  const translated = translateSiteMessage("zh_hans", noAvailableKeyMessage);

  assert.equal(
    translated,
    "当前账号没有可用的 Key。请先在站点为实际分组创建 Key，再重新同步。",
  );
  assert.equal(translated.includes("default"), false);
});

test("keeps legacy group-specific missing-key guidance", () => {
  const translated = translateSiteMessage(
    "zh_hans",
    'site sync requires a key for group "vip"; create a key for that group on the site and sync again',
  );

  assert.equal(
    translated,
    "分组「vip」没有可用的 Key。请先到站点创建这个分组的 Key，再重新同步。",
  );
});
