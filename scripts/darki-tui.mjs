import React, {useCallback, useEffect, useMemo, useRef, useState} from "react";
import {render, Box, Text, useApp, useInput, useStdout} from "ink";
import TextInput from "ink-text-input";
import {exec} from "node:child_process";
import {promisify} from "node:util";
import {readFile, writeFile, mkdir, readdir} from "node:fs/promises";
import {existsSync} from "node:fs";
import path from "node:path";

const execAsync = promisify(exec);
const BASE_URL = process.env.KIMI_PROXY_URL || "http://127.0.0.1:3001";
const MODEL = process.env.KIMI_MODEL || "kimi-k2.6";
const ROOT = process.env.DARKI_WORKSPACE || process.cwd();
const h = React.createElement;
const frames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"];
const YOLO_COLOR = {on: "#FF5C7A", off: "#7CFF9B"};
const SESSIONS_DIR = path.join(ROOT, "storage", "sessions");
const COMMANDS = [
  {cmd: "/new", desc: "limpa o chat e cria nova sessão"},
  {cmd: "/yolo", desc: "ativa/desativa YOLO mode (tools sem confirmação)"},
  {cmd: "/session", desc: "mostra sessões salvas (auto-save automático)"},
  {cmd: "/exit", desc: "sai do Darki TUI"},
  {cmd: "/quit", desc: "sai do Darki TUI"},
];
const title = [
  "██████╗  █████╗ ██████╗ ██╗  ██╗██╗",
  "██╔══██╗██╔══██╗██╔══██╗██║ ██╔╝██║",
  "██║  ██║███████║██████╔╝█████╔╝ ██║",
  "██║  ██║██╔══██║██╔══██╗██╔═██╗ ██║",
  "██████╔╝██║  ██║██║  ██║██║  ██╗██║",
  "╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝  ╚═╝╚═╝",
];

function stripAnsi(text) {
  return String(text).replace(/\x1b\[[0-9;]*m/g, "");
}

function truncate(text, max) {
  const value = String(text || "").replace(/\s+$/g, "");
  if (stripAnsi(value).length <= max) return value;
  return value.slice(0, Math.max(0, max - 1)) + "…";
}

function center(text, width) {
  const len = stripAnsi(text).length;
  if (len >= width) return text;
  return " ".repeat(Math.floor((width - len) / 2)) + text;
}

function roleStyle(role) {
  if (role === "user") return {color: "#7CFF9B", label: "YOU"};
  if (role === "assistant") return {color: "#E8E3FF", label: "DARKI"};
  if (role === "tool-call") return {color: "#FFD166", label: "CALL"};
  if (role === "tool") return {color: "#9BE7FF", label: "TOOL"};
  if (role === "error") return {color: "#FF5C7A", label: "ERR"};
  return {color: "#8A8F98", label: "SYS"};
}

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
  const normalized = String(input || ".").replaceAll("\\", "/");
  if (path.isAbsolute(normalized) || normalized.split("/").includes("..")) {
    throw new Error(`unsafe path: ${input}`);
  }
  const full = path.resolve(ROOT, normalized);
  if (full !== path.resolve(ROOT) && !full.startsWith(path.resolve(ROOT) + path.sep)) {
    throw new Error(`path outside workspace: ${input}`);
  }
  return full;
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

function globToRegex(pattern) {
  const escaped = pattern.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*").replace(/\?/g, ".");
  return new RegExp("^" + escaped + "$", "i");
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
    if (name === "edit" || name === "apply_patch") {
      const target = safePath(args.path);
      const text = await readFile(target, "utf8");
      if (!text.includes(args.old)) throw new Error("old text not found");
      await writeFile(target, text.replace(args.old, args.new));
      return `patched ${args.path}`;
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
    if (name === "question") return "(answer will be collected from user)";
    return `tool error: unknown tool ${name}`;
  } catch (error) {
    return `tool error: ${error.message}`;
  }
}

async function callKimi(messages) {
  const res = await fetch(`${BASE_URL}/v1/chat/completions`, {
    method: "POST",
    headers: {"content-type": "application/json"},
    body: JSON.stringify({model: MODEL, tool_choice: "auto", tools, messages}),
  });
  const text = await res.text();
  if (!res.ok) throw new Error(text);
  return JSON.parse(text);
}

async function newChat() {
  await fetch(`${BASE_URL}/new`, {method: "POST"}).catch(() => {});
}

async function ensureSessionsDir() {
  await mkdir(SESSIONS_DIR, {recursive: true});
}

async function listSessions() {
  await ensureSessionsDir();
  const entries = await readdir(SESSIONS_DIR, {withFileTypes: true});
  const files = entries.filter((e) => e.isFile() && e.name.endsWith(".json")).sort((a, b) => b.name.localeCompare(a.name));
  const sessions = [];
  for (const f of files) {
    try {
      const data = JSON.parse(await readFile(path.join(SESSIONS_DIR, f.name), "utf8"));
      sessions.push(data);
    } catch { /* skip corrupt */ }
  }
  return sessions;
}

async function autoSave(sessionId, messages) {
  if (messages.length < 2) return;
  await ensureSessionsDir();
  const file = path.join(SESSIONS_DIR, sessionId + ".json");
  const preview = messages.filter((m) => m.role !== "tool" && m.role !== "tool-call").slice(-1).map((m) => (m.content || "").slice(0, 80)).join(" ");
  const data = {sessionId, savedAt: Date.now(), messageCount: messages.length, preview, messages};
  await writeFile(file, JSON.stringify(data, null, 2));
}

function Spinner({busy, tick, yolo}) {
  if (busy) return h(Text, {color: "#FFD166"}, `${frames[tick % frames.length]} thinking/tools`);
  if (yolo) return h(Text, {color: "#FF5C7A", bold: true}, "⚡ YOLO");
  return h(Text, {color: "#7CFF9B"}, "● online");
}

function getMatches(prefix) {
  if (!prefix || !prefix.startsWith("/")) return [];
  const lower = prefix.toLowerCase();
  return COMMANDS.filter((c) => c.cmd.startsWith(lower));
}

function bestMatch(prefix) {
  const matches = getMatches(prefix);
  return matches.length === 1 ? matches[0].cmd : null;
}

function App() {
  const {exit} = useApp();
  const {stdout} = useStdout();
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [tick, setTick] = useState(0);
  const [yolo, setYolo] = useState(false);
  const [confirming, setConfirming] = useState(null);
  const [questioning, setQuestioning] = useState(null);
  const msgRef = useRef([]);
  const itemsRef = useRef([{role: "system", text: "Darki TUI ready. /new /yolo /exit"}]);
  const [displayItems, setDisplayItems] = useState(itemsRef.current);
  const [msgCount, setMsgCount] = useState(0);
  const confirmResolve = useRef(null);
  const yoloRef = useRef(false);
  const inputRef = useRef("");
  const storeRef = useRef({messages: [], items: []});
  const sessionIdRef = useRef(Date.now().toString(36) + Math.random().toString(36).slice(2, 6));

  const matches = getMatches(input);
  const shadow = bestMatch(input);
  const showCommands = input.startsWith("/") && !confirming && !questioning;

  const flush = useCallback(() => {
    if (storeRef.current.items.length > 0) {
      itemsRef.current = itemsRef.current.concat(storeRef.current.items);
      storeRef.current.items = [];
    }
    if (storeRef.current.messages.length > 0) {
      msgRef.current = storeRef.current.messages;
      storeRef.current.messages = [];
    }
    setDisplayItems(itemsRef.current.slice(-12));
    setMsgCount(msgRef.current.length);
  }, []);

  useEffect(() => { newChat(); }, []);
  useEffect(() => {
    if (!busy) return;
    const timer = setInterval(() => { setTick((value) => value + 1); }, 200);
    return () => clearInterval(timer);
  }, [busy]);

  useInput((_input, key) => {
    if (!key.tab || busy || confirming || questioning || !input.startsWith("/")) return;
    const complete = bestMatch(inputRef.current);
    if (complete) { setInput(complete + " "); }
  });

  const width = Math.max(72, Math.min(120, stdout?.columns || 96));
  const innerWidth = width - 6;

  function handleChange(value) {
    inputRef.current = value;
    setInput(value);
  }

  async function submit(value) {
    const text = value.trim();
    if (!text || busy) return;
    setInput("");
    inputRef.current = "";

    if (text === "/exit" || text === "/quit") return exit();
    if (text === "/yolo") {
      const next = !yoloRef.current;
      yoloRef.current = next;
      setYolo(next);
      storeRef.current.items = [{role: "system", text: next ? "⚡ YOLO mode ON — tools auto-executam" : "YOLO mode OFF — tools requerem confirmação"}];
      flush();
      return;
    }
    if (text === "/new") {
      await newChat();
      msgRef.current = [];
      storeRef.current.messages = [];
      sessionIdRef.current = Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
      storeRef.current.items = [{role: "system", text: "new chat created"}];
      flush();
      return;
    }
    if (text === "/session") {
      try {
        const sessions = await listSessions();
        if (sessions.length === 0) {
          storeRef.current.items = [{role: "system", text: "📁 nenhuma sessão salva ainda"}];
        } else {
          const lines = sessions.map((s) => {
            const date = s.savedAt ? new Date(s.savedAt).toLocaleString("pt-BR") : "?";
            const prev = s.preview ? ` "${s.preview}"` : "";
            return `${s.sessionId}  · ${s.messageCount || 0} msgs  · ${date}${prev}`;
          });
          storeRef.current.items = [{role: "system", text: `📁 Sessões (${sessions.length}):\n  ` + lines.join("\n  ")}];
        }
        flush();
      } catch (err) { storeRef.current.items = [{role: "error", text: err.message}]; flush(); }
      return;
    }

    setBusy(true);
    const userContent = text.toLowerCase().includes("darki") ? text : `darki\n${text}`;
    const userMessage = {role: "user", content: userContent};
    storeRef.current.messages = [...msgRef.current, userMessage];
    storeRef.current.items = [{role: "user", text}];
    flush();

    let nextMessages = msgRef.current;

    try {
      for (let step = 0; step < 8; step++) {
        const response = await callKimi(nextMessages);
        const message = response.choices?.[0]?.message || {};
        const toolCalls = message.tool_calls || message.toolCalls || [];
        if (toolCalls.length === 0) {
          const content = message.content || "";
          nextMessages = [...nextMessages, {role: "assistant", content}];
          storeRef.current.messages = nextMessages;
          storeRef.current.items = [{role: "assistant", text: content || "(empty)"}];
          flush();
          autoSave(sessionIdRef.current, nextMessages);
          break;
        }

        nextMessages = [...nextMessages, {role: "assistant", content: message.content || "", tool_calls: toolCalls}];
        const batch = [];
        for (const call of toolCalls) {
          const name = call.function?.name || "unknown";
          const argsPreview = (typeof call.function?.arguments === "string" ? call.function.arguments : JSON.stringify(call.function?.arguments || {})).slice(0, 600);
          batch.push({role: "tool-call", text: `${name} ${argsPreview}`});

          if (name === "question") {
            const q = (typeof call.function?.arguments === "string" ? JSON.parse(call.function?.arguments || "{}") : call.function?.arguments || {}).question || "?";
            storeRef.current.items = [...batch, {role: "tool-call", text: `❓ ${q}`}];
            flush();
            setQuestioning(q);
            const userAnswer = await new Promise((resolve) => { confirmResolve.current = resolve; });
            setQuestioning(null);
            if (userAnswer === null) {
              nextMessages = [...nextMessages, {role: "tool", tool_call_id: call.id, content: "user dismissed the question"}];
              batch.push({role: "tool", text: "⨯ user dismissed"});
              continue;
            }
            nextMessages = [...nextMessages, {role: "tool", tool_call_id: call.id, content: userAnswer}];
            batch.push({role: "tool", text: `→ ${userAnswer.slice(0, 600)}`});
            continue;
          }

          if (!yoloRef.current) {
            storeRef.current.items = batch;
            flush();
            setConfirming({name, args: argsPreview});
            const answer = await new Promise((resolve) => { confirmResolve.current = resolve; });
            setConfirming(null);
            if (!answer) {
              nextMessages = [...nextMessages, {role: "tool", tool_call_id: call.id, content: "user denied this tool call"}];
              batch.push({role: "tool", text: "⨯ denied by user"});
              continue;
            }
          }

          const result = await executeTool(call);
          nextMessages = [...nextMessages, {role: "tool", tool_call_id: call.id, content: result}];
          batch.push({role: "tool", text: result.slice(0, 1200)});
        }
        storeRef.current.messages = nextMessages;
        storeRef.current.items = batch;
        flush();
        autoSave(sessionIdRef.current, nextMessages);
      }
    } catch (error) {
      storeRef.current.items = [{role: "error", text: error.message}];
      flush();
    } finally {
      if (msgRef.current.length > 1) autoSave(sessionIdRef.current, msgRef.current);
      setBusy(false);
    }
  }

  function handlePrompt(value) {
    if (questioning) {
      confirmResolve.current?.(value || null);
      setInput("");
      inputRef.current = "";
      return;
    }
    handleConfirm(value);
  }

  function handleConfirm(key) {
    if (key === "y" || key === "Y" || key === "") confirmResolve.current?.(true);
    else if (key === "a" || key === "A") { yoloRef.current = true; setYolo(true); confirmResolve.current?.(true); }
    else confirmResolve.current?.(false);
  }

  return h(Box, {flexDirection: "column", width},
    h(Box, {flexDirection: "column", marginBottom: 1},
      ...title.map((line, index) => h(Text, {key: index, color: "#FF2040", bold: true}, center(line, width))),
      h(Text, {color: "#8A8F98"}, center(`Kimi ${MODEL}  •  OpenAI tool calling  •  ${BASE_URL}`, width)),
    ),

    h(Box, {borderStyle: "round", borderColor: busy ? "#FFD166" : yolo ? "#FF5C7A" : "#6D5DFB", flexDirection: "column", paddingX: 1, minHeight: 16},
      h(Box, {justifyContent: "space-between", marginBottom: 1},
        h(Text, {color: "#A78BFA", bold: true}, yolo ? " ⚡ DARKI YOLO " : " DARKI SESSION "),
        h(Spinner, {busy, tick, yolo})
      ),
      ...displayItems.map((item, index) => {
        const style = roleStyle(item.role);
        const lines = String(item.text || "").split(/\r?\n/).slice(0, 6);
        return h(Box, {key: `${msgCount}-${index}`, flexDirection: "column", marginBottom: 1},
          h(Text, {color: style.color, bold: true}, ` ${style.label} `),
          ...lines.map((line, lineIndex) => h(Text, {key: lineIndex, color: style.color}, `  ${truncate(line, innerWidth)}`))
        );
      }),
      confirming ? h(Box, {marginTop: 1, flexDirection: "column"},
        h(Text, {color: "#FFD166", bold: true}, ` ⚠ ${confirming.name}`),
        h(Text, {color: "#8A8F98"}, `  ${truncate(confirming.args, innerWidth - 2)}`),
        h(Text, {color: "#7CFF9B"}, `  [Y]es  [N]o  [A]llow always (YOLO)`),
      ) : null,
      questioning ? h(Box, {marginTop: 1, flexDirection: "column"},
        h(Text, {color: "#67E8F9", bold: true}, ` ❓ ${truncate(questioning, innerWidth - 2)}`),
        h(Text, {color: "#7CFF9B"}, `  Digite sua resposta e pressione Enter`),
      ) : null
    ),

    h(Box, {flexDirection: "column", marginTop: 1},
      h(Box, {borderStyle: "round", borderColor: busy ? "#555" : yolo ? "#FF5C7A" : "#67E8F9", paddingX: 1, flexDirection: "column"},
        h(Box, {},
          h(Text, {color: busy ? "#FFD166" : yolo ? "#FF5C7A" : "#67E8F9", bold: true}, questioning ? "✎ " : confirming ? "? " : busy ? `${frames[tick % frames.length]} ` : "› "),
          questioning
            ? h(TextInput, {value: input, onChange: handleChange, onSubmit: (v) => { handlePrompt(v.trim()); }, placeholder: "Sua resposta…"})
            : confirming
              ? h(TextInput, {value: input, onChange: handleChange, onSubmit: (v) => { handlePrompt(v.trim()); }, placeholder: "Run tool? (Y/n/a)"})
              : h(TextInput, {value: input, onChange: handleChange, onSubmit: submit, placeholder: busy ? "Darki is working…" : "Ask Darki…"})
        ),
        showCommands && !confirming
          ? h(Box, {marginTop: 1, flexDirection: "column", borderStyle: "round", borderColor: "#555", paddingX: 1},
              ...matches.map((m) =>
                h(Box, {key: m.cmd, gap: 1},
                  h(Text, {color: "#FFD166", bold: true}, ` ${m.cmd}`),
                  h(Text, {color: "#8A8F98"}, m.desc),
                )
              ),
              matches.length === 0
                ? h(Text, {color: "#FF5C7A"}, `  no matching command`)
                : shadow && input !== shadow
                  ? h(Text, {color: "#555"}, `  ⇥ Tab:  ${shadow}`)
                  : null
            )
          : null
      ),
      h(Box, {marginTop: 1, justifyContent: "space-between"},
        h(Text, {color: "#555"}, truncate(`workspace ${ROOT}`, Math.floor(width * 0.65))),
        h(Text, {color: yolo ? "#FF5C7A" : "#555"}, yolo ? "⚡ YOLO" : `${msgCount} msgs`)
      )
    )
  );
}

if (!process.stdin.isTTY) {
  console.error("Darki TUI precisa ser executado em um terminal interativo com TTY. Rode: npm run darki");
  process.exit(1);
}

render(h(App));
