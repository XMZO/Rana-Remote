import { readFileSync, readdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __filename = fileURLToPath(import.meta.url);
const localeDir = resolve(dirname(__filename), "..", "app", "locales");
const files = readdirSync(localeDir).filter((f) => f.endsWith(".json"));
if (files.length < 2) {
  throw new Error("need at least two locale files");
}
const maps = files.map((f) => JSON.parse(readFileSync(resolve(localeDir, f), "utf8")));
const baseKeys = Object.keys(maps[0]).sort();
for (let i = 1; i < maps.length; i++) {
  const keys = Object.keys(maps[i]).sort();
  if (JSON.stringify(baseKeys) !== JSON.stringify(keys)) {
    throw new Error(`i18n key mismatch between ${files[0]} and ${files[i]}`);
  }
}
console.log("i18n keys aligned:", files.join(", "));
