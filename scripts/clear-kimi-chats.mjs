import { chromium } from "playwright";
import { readFile } from "node:fs/promises";
import process from "node:process";

const profileDir = ".playwright/kimi-profile";
const storageStatePath = process.env.KIMI_STORAGE_STATE || "storage/kimi-state.json";

async function sleep(ms) {
  return new Promise(r => setTimeout(r, ms));
}

export async function clearKimiChats() {
  const state = JSON.parse(await readFile(storageStatePath, "utf-8"));

  const context = await chromium.launchPersistentContext(profileDir, {
    headless: true,
    channel: process.env.KIMI_BROWSER_CHANNEL || "chrome",
    viewport: { width: 1280, height: 900 },
  });

  const page = context.pages()[0] || await context.newPage();

  await context.addCookies(state.cookies);
  await page.goto("https://www.kimi.com/chat/history", { waitUntil: "domcontentloaded" });
  await page.waitForTimeout(3000);

  let remaining = Infinity;
  for (let round = 0; round < 10 && remaining > 0; round++) {
    remaining = await page.evaluate(async () => {
      const cbs = document.querySelectorAll('.history-checkbox-container input[type=checkbox]');
      if (cbs.length === 0) return 0;
      cbs.forEach(cb => { cb.checked = true; cb.dispatchEvent(new Event('change', {bubbles: true})); });
      await new Promise(r => setTimeout(r, 500));
      document.querySelector('.delete.horizontal.action-hover')?.click();
      await new Promise(r => setTimeout(r, 800));
      document.querySelector('.km-button-danger')?.click();
      await new Promise(r => setTimeout(r, 3000));
      return cbs.length;
    });
  }

  await context.close();
  return remaining === 0;
}

if (process.argv[1] === import.meta.filename) {
  clearKimiChats()
    .then(ok => { console.log(ok ? "all chats cleared" : "some chats remaining"); process.exit(ok ? 0 : 1); })
    .catch(err => { console.error("error:", err.message); process.exit(1); });
}
