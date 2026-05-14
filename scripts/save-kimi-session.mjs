import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";

const profileDir = ".playwright/kimi-profile";
const outputPath = process.env.KIMI_STORAGE_STATE || "storage/kimi-state.json";
const loginUrl = process.env.KIMI_LOGIN_URL || "https://www.kimi.com/";
const timeoutMs = Number(process.env.KIMI_LOGIN_TIMEOUT_MS || 180000);

await mkdir("storage", { recursive: true });

const context = await chromium.launchPersistentContext(profileDir, {
  headless: false,
  viewport: { width: 1280, height: 900 },
});

const page = context.pages()[0] || await context.newPage();
await page.goto(loginUrl, { waitUntil: "domcontentloaded" });

console.log("Chromium aberto no Kimi. Faça login se necessário.");
console.log(`Aguardando cookie kimi-auth por até ${Math.round(timeoutMs / 1000)}s...`);

const deadline = Date.now() + timeoutMs;
let authed = false;
while (Date.now() < deadline) {
  const cookies = await context.cookies();
  authed = cookies.some((cookie) => cookie.name === "kimi-auth" && /(^|\.)kimi\.com$/.test(cookie.domain));
  if (authed) break;
  await page.waitForTimeout(1000);
}

if (!authed) {
  await context.close();
  throw new Error("Timeout aguardando login do Kimi: cookie kimi-auth não apareceu.");
}

await page.goto("https://www.kimi.com/", { waitUntil: "domcontentloaded" }).catch(() => {});
await page.waitForTimeout(2000);

const state = await context.storageState();
const filteredState = {
  cookies: state.cookies.filter((cookie) => {
    const domain = cookie.domain.replace(/^\./, "").toLowerCase();
    return domain === "kimi.com" || domain === "www.kimi.com";
  }),
  origins: state.origins.filter((origin) => {
    return origin.origin === "https://www.kimi.com" || origin.origin === "https://kimi.com";
  }),
};

await writeFile(outputPath, JSON.stringify(filteredState, null, 2));
await context.close();

console.log(`Sessão Kimi salva em ${outputPath}`);
console.log("O arquivo contém apenas estado relacionado a kimi.com/www.kimi.com.");
