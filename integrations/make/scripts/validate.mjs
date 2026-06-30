#!/usr/bin/env node
// Structural validator for the Warmbly Make.com Custom App.
// IML is interpreted by Make (no compiler), so this is the CI gate: it checks
// the manifest, JSON validity, code-file references, codeFiles keys, component
// enums, and that nothing is orphaned. Exits non-zero on any problem.

import { readFileSync, readdirSync, statSync, existsSync } from 'node:fs';
import { join, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = join(dirname(fileURLToPath(import.meta.url)), '..');
const errors = [];
const err = (m) => errors.push(m);

// Allowed codeFiles keys per component type (Make Apps SDK ComponentCodeType).
const ALLOWED_KEYS = {
  connection: ['communication', 'params', 'common', 'scopeList', 'defaultScope', 'installSpec', 'installDirectives'],
  module: ['communication', 'epoch', 'staticParams', 'mappableParams', 'interface', 'samples', 'scope'],
  rpc: ['communication', 'params'],
  webhook: ['communication', 'params', 'attach', 'detach', 'update', 'requiredScope'],
  function: ['code', 'test'],
};
const GENERAL_KEYS = ['base', 'common', 'readme', 'groups'];
const MODULE_TYPES = ['trigger', 'action', 'search', 'instant_trigger', 'responder', 'universal'];
const CRUD = ['create', 'read', 'update', 'delete'];
const CONNECTION_TYPES = ['basic', 'oauth', 'apikey', 'digest'];

const readJson = (rel) => {
  const abs = join(ROOT, rel);
  const text = readFileSync(abs, 'utf8');
  return JSON.parse(text);
};

// 1. Manifest parses.
let manifest;
try {
  manifest = readJson('makecomapp.json');
} catch (e) {
  console.error('FATAL: makecomapp.json is missing or invalid JSON:', e.message);
  process.exit(1);
}

const referenced = new Set(); // every code-file path the manifest points at

// 2. General code files.
for (const [key, path] of Object.entries(manifest.generalCodeFiles || {})) {
  if (!GENERAL_KEYS.includes(key)) err(`generalCodeFiles: unknown key "${key}"`);
  referenced.add(path);
  if (!existsSync(join(ROOT, path))) err(`generalCodeFiles.${key}: missing file ${path}`);
}

// 3. Components.
const components = manifest.components || {};
for (const type of Object.keys(components)) {
  if (!ALLOWED_KEYS[type]) {
    err(`components: unknown component type "${type}"`);
    continue;
  }
  for (const [name, def] of Object.entries(components[type] || {})) {
    // enum checks
    if (type === 'module') {
      if (!MODULE_TYPES.includes(def.moduleType)) err(`module ${name}: invalid moduleType "${def.moduleType}"`);
      if (def.moduleType === 'action' && def.actionCrud && !CRUD.includes(def.actionCrud)) {
        err(`module ${name}: invalid actionCrud "${def.actionCrud}"`);
      }
    }
    if (type === 'connection' && !CONNECTION_TYPES.includes(def.connectionType)) {
      err(`connection ${name}: invalid connectionType "${def.connectionType}"`);
    }
    // codeFiles
    const codeFiles = def.codeFiles || {};
    if (!codeFiles.communication && type !== 'function') {
      err(`${type} ${name}: missing required "communication" code file`);
    }
    if (type === 'function' && !codeFiles.code) {
      err(`function ${name}: missing required "code" file`);
    }
    for (const [key, path] of Object.entries(codeFiles)) {
      if (!ALLOWED_KEYS[type].includes(key)) err(`${type} ${name}: invalid codeFiles key "${key}"`);
      referenced.add(path);
      if (!existsSync(join(ROOT, path))) err(`${type} ${name}: missing file ${path} (key "${key}")`);
    }
  }
}

// 4. Every referenced .iml.json / .json parses; every .js parses.
for (const path of referenced) {
  const abs = join(ROOT, path);
  if (!existsSync(abs)) continue; // already reported
  if (path.endsWith('.json')) {
    try {
      JSON.parse(readFileSync(abs, 'utf8'));
    } catch (e) {
      err(`invalid JSON: ${path} (${e.message})`);
    }
  } else if (path.endsWith('.js')) {
    try {
      // Function-body parse check; Make runs these in its own sandbox.
      new Function(readFileSync(abs, 'utf8'));
    } catch (e) {
      err(`invalid JS: ${path} (${e.message})`);
    }
  }
}

// 5. No orphaned files in component directories.
const COMPONENT_DIRS = ['general', 'connections', 'rpcs', 'modules', 'functions'];
const walk = (dir) => {
  const abs = join(ROOT, dir);
  if (!existsSync(abs)) return [];
  return readdirSync(abs).flatMap((entry) => {
    const full = join(abs, entry);
    return statSync(full).isDirectory() ? walk(relative(ROOT, full)) : [relative(ROOT, full)];
  });
};
for (const dir of COMPONENT_DIRS) {
  for (const file of walk(dir)) {
    if (!referenced.has(file)) err(`orphaned file (not referenced by makecomapp.json): ${file}`);
  }
}

// Report.
if (errors.length) {
  console.error(`Make app validation FAILED with ${errors.length} problem(s):`);
  for (const e of errors) console.error('  - ' + e);
  process.exit(1);
}
const counts = {
  modules: Object.keys(components.module || {}).length,
  rpcs: Object.keys(components.rpc || {}).length,
  connections: Object.keys(components.connection || {}).length,
  functions: Object.keys(components.function || {}).length,
};
console.log(
  `Make app OK: ${counts.modules} modules, ${counts.rpcs} RPCs, ${counts.connections} connection(s), ` +
    `${counts.functions} function(s); ${referenced.size} code files referenced and present.`
);
