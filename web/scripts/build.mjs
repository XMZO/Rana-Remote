import { cpSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const root = resolve(dirname(__filename), "..");
const publicDir = resolve(root, "public");
const localesDir = resolve(root, "app", "locales");
const distDir = resolve(root, "dist");
mkdirSync(distDir, { recursive: true });
cpSync(publicDir, distDir, { recursive: true });
cpSync(localesDir, resolve(distDir, "locales"), { recursive: true });
console.log("built web dist");
