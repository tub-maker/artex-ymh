/* ARTEX service worker — 保守策略，只为 PWA 可安装 + 静态资源离线兜底。
 *
 * 设计约束（重要）：
 * 1. 绝不拦截 /api/* —— 包括 SSE 流（/api/.../stream）。SSE 必须直连，
 *    任何缓冲或缓存都会让事件帧丢失。
 * 2. 绝不拦截伪装门控路径 /g-*。
 * 3. 只缓存同源、GET、且是构建产物的静态资源。
 * 4. 缓存失败不影响页面可用性：所有 catch 都回落到网络。
 */

const VERSION = "artex-v1";
const STATIC_CACHE = `${VERSION}-static`;

/** 只缓存这些前缀下的 GET 资源（Next 构建产物 + 图标）。 */
const CACHEABLE_PREFIXES = ["/_next/static/", "/icons/"];

/** 明确永不缓存/拦截的路径前缀。 */
const BYPASS_PREFIXES = ["/api/", "/g-", "/s/"];

self.addEventListener("install", (event) => {
  // 不做 precache：构建产物的文件名带 hash，交给运行时缓存。
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      // 清理旧版本缓存
      const keys = await caches.keys();
      await Promise.all(keys.filter((k) => !k.startsWith(VERSION)).map((k) => caches.delete(k)));
      await self.clients.claim();
    })(),
  );
});

self.addEventListener("fetch", (event) => {
  const request = event.request;

  // 只处理同源 GET
  if (request.method !== "GET") return;

  let url;
  try {
    url = new URL(request.url);
  } catch {
    return;
  }
  if (url.origin !== self.location.origin) return;

  // 明确放行：API、SSE、门控、暂存下载
  if (BYPASS_PREFIXES.some((prefix) => url.pathname.startsWith(prefix))) return;

  // 静态资源：cache-first，回落到网络
  if (CACHEABLE_PREFIXES.some((prefix) => url.pathname.startsWith(prefix))) {
    event.respondWith(
      (async () => {
        const cache = await caches.open(STATIC_CACHE);
        const hit = await cache.match(request);
        if (hit) return hit;
        try {
          const response = await fetch(request);
          if (response.ok && response.type === "basic") {
            cache.put(request, response.clone()).catch(() => {});
          }
          return response;
        } catch {
          return hit ?? Response.error();
        }
      })(),
    );
    return;
  }

  // 页面导航：network-first，离线时回落到缓存
  if (request.mode === "navigate") {
    event.respondWith(
      (async () => {
        try {
          return await fetch(request);
        } catch {
          const cache = await caches.open(STATIC_CACHE);
          const cached = await cache.match(request);
          if (cached) return cached;
          throw new Error("offline");
        }
      })(),
    );
  }

  // 其余请求走默认网络行为
});
