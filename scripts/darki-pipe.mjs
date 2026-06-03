import {exec} from "node:child_process";
import {promisify} from "node:util";
import {readFile, writeFile, mkdir, readdir} from "node:fs/promises";
import {existsSync} from "node:fs";
import path from "node:path";

const execAsync = promisify(exec);
const BASE_URL = process.env.KIMI_PROXY_URL || "http://127.0.0.1:3001";
const MODEL = process.env.KIMI_MODEL || "kimi-k2.6";
const ROOT = process.env.DARKI_WORKSPACE || process.cwd();

const tools = [
  {type: "function", function: {name: "bash", description: "Run a non-interactive cmd.exe command in the workspace", parameters: {type: "object", properties: {command: {type: "string"}, timeout_ms: {type: "integer"}}, required: ["command"]}}},
  {type: "function", function: {name: "read_file", description: "Read a text file from the workspace", parameters: {type: "object", properties: {path: {type: "string"}}, required: ["path"]}}},
  {type: "function", function: {name: "write_file", description: "Write a text file inside the workspace", parameters: {type: "object", properties: {path: {type: "string"}, content: {type: "string"}}, required: ["path", "content"]}}},
  {type: "function", function: {name: "list_files", description: "List files under a directory in the workspace", parameters: {type: "object", properties: {path: {type: "string"}}, required: []}}},
  {type: "function", function: {name: "glob", description: "Find files matching a glob pattern (ex: **/*.ts, src/**/util.*)", parameters: {type: "object", properties: {pattern: {type: "string"}, path: {type: "string"}}, required: ["pattern"]}}},
  {type: "function", function: {name: "grep", description: "Search file contents by regular expression", parameters: {type: "object", properties: {pattern: {type: "string"}, path: {type: "string"}}, required: ["pattern"]}}},
  {type: "function", function: {name: "edit", description: "Replace exact text inside a file", parameters: {type: "object", properties: {path: {type: "string"}, old: {type: "string"}, new: {type: "string"}}, required: ["path", "old", "new"]}}},
  {type: "function", function: {name: "web_fetch", description: "Fetch text content from a specific URL", parameters: {type: "object", properties: {url: {type: "string"}}, required: ["url"]}}},
  {type: "function", function: {name: "web_search", description: "Search the web for current information", parameters: {type: "object", properties: {query: {type: "string"}}, required: ["query"]}}},
  {type: "function", function: {name: "question", description: "Ask the user a question and wait for their answer. Use for clarifications, decisions, or gathering info.", parameters: {type: "object", properties: {question: {type: "string"}}, required: ["question"]}}},
];

function safePath(input = ".") {
  const full = path.resolve(ROOT, String(input || "."));
  const rootResolved = path.resolve(ROOT);
  if (full !== rootResolved && !full.startsWith(rootResolved + path.sep)) {
    throw new Error(`path outside workspace: ${input}`);
  }
  return full;
}

function globToRegex(pattern) {
  const escaped = pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".");
  return new RegExp("^" + escaped + "$", "i");
}

async function walk(dir, limit = 300) {
  const out = [];
  async function visit(current) {
    if (out.length >= limit) return;
    for (const entry of await readdir(current, {withFileTypes: true})) {
      if (out.length >= limit) return;
      if ([".git", "node_modules", ".playwright", "storage"].includes(entry.name)) continue;
      const full = path.join(current, entry.name);
      const rel = path.relative(ROOT, full).replaceAll("\\", "/");
      out.push(entry.isDirectory() ? `${rel}/` : rel);
      if (entry.isDirectory()) await visit(full);
    }
  }
  await visit(dir);
  return out.join("\n");
}

async function executeTool(call) {
  const name = call?.function?.name;
  let args = call?.function?.arguments || "{}";
  if (typeof args === "string") args = JSON.parse(args || "{}");

  try {
    if (name === "bash" || name === "run_command") {
      const timeout = Math.min(Number(args.timeout_ms || args.timeout || 30000), 120000);
      const {stdout, stderr} = await execAsync(args.command, {cwd: ROOT, timeout, windowsHide: true, maxBuffer: 1024 * 1024 * 5});
      return [stdout, stderr].filter(Boolean).join("\n") || "command completed";
    }
    if (name === "read_file") return await readFile(safePath(args.path), "utf8");
    if (name === "write_file") {
      const target = safePath(args.path);
      await mkdir(path.dirname(target), {recursive: true});
      await writeFile(target, String(args.content ?? ""));
      return `wrote ${args.path}`;
    }
    if (name === "list_files") return await walk(safePath(args.path || "."));
    if (name === "glob") {
      const dir = safePath(args.path || ".");
      const re = globToRegex(args.pattern);
      const all = (await walk(dir, 500)).split("\n").filter(Boolean);
      return all.filter((f) => re.test(f)).join("\n") || "no matches";
    }
    if (name === "grep") {
      const dir = safePath(args.path || ".");
      const pattern = new RegExp(String(args.pattern), "i");
      const files = (await walk(dir, 500)).split("\n").filter(Boolean).filter((f) => !f.endsWith("/"));
      const hits = [];
      for (const file of files.slice(0, 300)) {
        const full = safePath(file);
        if (!existsSync(full)) continue;
        let text = "";
        try { text = await readFile(full, "utf8"); } catch { continue; }
        const lines = text.split(/\r?\n/);
        lines.forEach((line, index) => { if (pattern.test(line)) hits.push(`${file}:${index + 1}: ${line}`); });
        if (hits.length >= 100) return hits.join("\n");
      }
      return hits.join("\n") || "no matches";
    }
    if (name === "web_fetch") {
      const res = await fetch(args.url);
      return (await res.text()).slice(0, 20000);
    }
    if (name === "web_search") {
      const res = await fetch(`https://api.duckduckgo.com/?q=${encodeURIComponent(args.query)}&format=json&no_html=1&skip_disambig=1`);
      const data = await res.json();
      const results = [];
      if (data.AbstractText) results.push(data.AbstractText);
      if (data.Results) data.Results.slice(0, 8).forEach((r) => { if (r.Text) results.push(r.Text); });
      return results.join("\n") || "no results";
    }
    if (name === "edit" || name === "apply_patch") {
      const target = safePath(args.path);
      const text = await readFile(target, "utf8");
      if (!text.includes(args.old)) throw new Error("old text not found");
      await writeFile(target, text.replace(args.old, args.new));
      return `patched ${args.path}`;
    }
    if (name === "clear_chats") {
      const res = await fetch(`${BASE_URL}/v1/clear-chats`, {method:"POST", headers:{"content-type":"application/json"}, body:"{}"});
      const data = await res.json();
      if (!res.ok) throw new Error(data.error?.message || res.statusText);
      return "all chats cleared";
    }
    if (name === "question") {
      process.stderr.write(`\n\x1b[36m❓ ${args.question || "?"}\x1b[0m\n\x1b[33mResposta: \x1b[0m`);
      return new Promise((resolve) => {
        const wasRaw = process.stdin.isRaw;
        if (wasRaw) process.stdin.setRawMode(false);
        process.stdin.once("data", (buffer) => {
          const answer = buffer.toString().trim();
          if (wasRaw) process.stdin.setRawMode(true);
          process.stderr.write("\x1b[32m✓ answer captured\x1b[0m\n");
          resolve(answer || "no answer");
        });
      });
    }
    return `tool error: unknown tool ${name}`;
  } catch (error) {
    return `tool error: ${error.message}`;
  }
}

async function callKimi(messages) {
  const res = await fetch(`${BASE_URL}/v1/chat/completions`, {
    method: "POST",
    headers: {"content-type": "application/json", "X-Kimi-Auto-Tools": "false"},
    body: JSON.stringify({model: MODEL, tool_choice: "auto", tools, messages}),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text);
  return JSON.parse(text);
}

async function newChat() {
  await fetch(`${BASE_URL}/new`, {method: "POST"}).catch(() => {});
}

async function confirmTool(call, yolo) {
  if (yolo) return true;
  const name = call.function?.name || "unknown";
  const args = typeof call.function?.arguments === "string" ? call.function.arguments : JSON.stringify(call.function?.arguments || {});
  const preview = args.length > 500 ? args.slice(0, 500) + "..." : args;
  process.stderr.write(`\n\x1b[33m⚠ Ferramenta: ${name}\x1b[0m\n\x1b[90m${preview}\x1b[0m\n\x1b[36mExecutar? [Y/n/q] \x1b[0m`);
  return new Promise((resolve) => {
    const wasRaw = process.stdin.isRaw;
    if (wasRaw) process.stdin.setRawMode(false);
    process.stdin.once("data", (buffer) => {
      const key = buffer.toString().trim().toLowerCase();
      if (wasRaw) process.stdin.setRawMode(true);
      if (key === "n" || key === "no") resolve(false);
      else if (key === "q" || key === "quit") { process.exit(0); }
      else resolve(true);
    });
  });
}

const SESSIONS_DIR = path.join(ROOT, "storage", "sessions");

async function ensureSessionsDir() {
  await mkdir(SESSIONS_DIR, {recursive: true});
}

async function autoSave(sessionId, messages) {
  if (messages.length < 2) return;
  await ensureSessionsDir();
  const file = path.join(SESSIONS_DIR, sessionId + ".json");
  const preview = messages.filter((m) => m.role !== "tool" && m.role !== "tool-call").slice(-1).map((m) => (m.content || "").slice(0, 80)).join(" ");
  const data = {sessionId, savedAt: Date.now(), messageCount: messages.length, preview, messages};
  await writeFile(file, JSON.stringify(data, null, 2));
}

async function main() {
  const args = process.argv.slice(2);
  let message;
  const yolo = args.includes("-y") || args.includes("--yolo");

  const cleanArgs = args.filter((a) => a !== "-y" && a !== "--yolo");

  if (cleanArgs.includes("-p")) {
    const idx = cleanArgs.indexOf("-p");
    if (idx + 1 < cleanArgs.length) {
      message = cleanArgs[idx + 1];
    } else {
      message = await new Promise((resolve) => {
        let data = "";
        process.stdin.on("data", (chunk) => { data += chunk.toString(); });
        process.stdin.on("end", () => resolve(data.trim()));
      });
    }
  } else if (cleanArgs.length > 0 && !cleanArgs[0].startsWith("-")) {
    message = cleanArgs.join(" ");
  } else {
    message = await new Promise((resolve) => {
      let data = "";
      process.stdin.on("data", (chunk) => { data += chunk.toString(); });
      process.stdin.on("end", () => resolve(data.trim()));
    });
  }

  if (!message) {
    console.error("Usage: node scripts/darki-pipe.mjs [-y] -p \"your message\"");
    console.error("   or: echo \"message\" | node scripts/darki-pipe.mjs [-y]");
    console.error("   or: node scripts/darki-pipe.mjs [-y] \"your message\"");
    console.error("   -y  YOLO mode: executa ferramentas sem confirmar");
    process.exit(1);
  }

  const sessionId = Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
  await newChat();
  const userMessage = {role: "user", content: message.toLowerCase().includes("darki") ? message : `darki\n${message}`};
  let messages = [userMessage];

  for (let step = 0; step < 8; step++) {
    const response = await callKimi(messages);
    const choice = response.choices?.[0]?.message || {};
    const toolCalls = choice.tool_calls || choice.toolCalls || [];

    if (toolCalls.length === 0) {
      messages = [...messages, {role: "assistant", content: choice.content || ""}];
      await autoSave(sessionId, messages);
      process.stdout.write(choice.content || "");
      process.exit(0);
    }

    messages = [...messages, {role: "assistant", content: choice.content || "", tool_calls: toolCalls}];
    for (const call of toolCalls) {
      const ok = await confirmTool(call, yolo);
      if (!ok) {
        messages = [...messages, {role: "tool", tool_call_id: call.id, content: "user denied this tool call"}];
        process.stderr.write("\x1b[33m⨯ tool denied\x1b[0m\n");
        continue;
      }
      process.stderr.write("\x1b[32m✓ running...\x1b[0m\n");
      const result = await executeTool(call);
      messages = [...messages, {role: "tool", tool_call_id: call.id, content: result}];
    }
    autoSave(sessionId, messages);
  }

  process.stdout.write("tool loop exceeded max steps");
  process.exit(1);
}

main().catch((err) => {
  console.error("pipe error:", err.message);
  process.exit(1);
});
