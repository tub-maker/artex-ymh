"use client";

import { useEffect } from "react";

/**
 * 注册 Service Worker（PWA 离线兜底 + 可安装）。
 *
 * 只在安全上下文里注册：
 * - HTTPS（隧道 / 反向代理 / 私有 CA）
 * - 或 localhost
 *
 * 局域网 HTTP（如 http://192.168.1.10:8787）不是安全上下文，浏览器会拒绝
 * 注册 Service Worker —— 这里静默跳过，页面功能不受影响。
 *
 * 失败一律吞掉：SW 只是增强，不能因为它让控制台起不来。
 */
export function ServiceWorkerRegister() {
  useEffect(() => {
    if (typeof window === "undefined") return;
    if (!("serviceWorker" in navigator)) return;

    const secure =
      window.location.protocol === "https:" ||
      window.location.hostname === "localhost" ||
      window.location.hostname === "127.0.0.1";
    if (!secure) return;

    const register = () => {
      navigator.serviceWorker.register("/sw.js").catch(() => {
        /* 注册失败不影响页面 */
      });
    };

    if (document.readyState === "complete") {
      register();
      return;
    }
    window.addEventListener("load", register, { once: true });
    return () => window.removeEventListener("load", register);
  }, []);

  return null;
}
