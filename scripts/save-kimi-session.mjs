import { chromium } from "playwright";
import { mkdir, writeFile } from "node:fs/promises";
import readline from "node:readline/promises";
import { stdin as input, stdout as output } from "node:process";

const profileDir = ".playwright/kimi-profile";
const outputPath = process.env.KIMI_STORAGE_STATE || "storage/kimi-state.json";
const loginUrl = process.env.KIMI_LOGIN_URL || "https://www.kimi.com/";
const timeoutMs = Number(process.env.KIMI_LOGIN_TIMEOUT_MS || 180000);

await mkdir("storage", { recursive: true });

const context = await chromium.launchPersistentContext(profileDir, {
  headless: false,
  channel: process.env.KIMI_BROWSER_CHANNEL || "chrome",
  viewport: { width: 1280, height: 900 },
  locale: "pt-BR",
  timezoneId: "America/Sao_Paulo",
  ignoreDefaultArgs: ["--enable-automation"],
  args: [
    "--disable-blink-features=AutomationControlled",
    "--disable-infobars",
    "--no-default-browser-check",
    "--no-first-run",
    "--start-maximized",
  ],
});

const page = context.pages()[0] || await context.newPage();
await page.goto(loginUrl, { waitUntil: "domcontentloaded" });

await context.clearCookies().catch(() => {});
await page.evaluate(() => {
  localStorage.clear();
  sessionStorage.clear();
}).catch(() => {});
await page.goto(loginUrl, { waitUntil: "domcontentloaded" });

console.log("Chromium aberto no Kimi com sessão limpa.");
console.log("Faça login na conta certa. Depois que o chat abrir/logar, volte aqui e aperte Enter.");

const rl = readline.createInterface({ input, output });
await rl.question("Pressione Enter somente depois de concluir o login no Kimi...");
rl.close();

console.log(`Validando cookie kimi-auth por até ${Math.round(timeoutMs / 1000)}s...`);

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
const hasAnonymousToken = state.origins.some((origin) => {
  return (origin.localStorage || []).some((item) => item.name === "anonymous_access_token" || item.name === "anonymous_refresh_token");
});
if (hasAnonymousToken) {
  console.warn("Aviso: ainda existem tokens anonimos no storage. Se o proxy pedir login, repita e confirme que a conta aparece logada no Kimi antes de apertar Enter.");
}
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
