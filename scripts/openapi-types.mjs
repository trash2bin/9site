// Генератор TS-типов из спеки OpenAPI: спека собрана из Go-структур, поэтому
// это единственный путь, которым фронт узнаёт форму ответа.

import { readFileSync, writeFileSync } from "node:fs";

const specPath = process.argv[2] ?? "server/internal/api/openapi.json";
const outPath = process.argv[3] ?? "src/api-types.ts";

const spec = JSON.parse(readFileSync(specPath, "utf8"));
const schemas = spec?.components?.schemas;
if (!schemas || typeof schemas !== "object" || Object.keys(schemas).length === 0) {
  throw new Error(`${specPath}: нет components.schemas`);
}

/** Имя типа из ссылки: "#/components/schemas/Photo" -> "Photo". */
function refName(ref) {
  const name = ref.split("/").pop();
  if (!name) {
    throw new Error(`пустая ссылка ${ref}`);
  }
  return name;
}

/** TS-тип по схеме; на незнакомой конструкции падаем, а не пишем any. */
function tsType(schema, indent = "") {
  if (!schema || typeof schema !== "object") {
    throw new Error("схема не объект");
  }
  if (typeof schema.$ref === "string") {
    return refName(schema.$ref);
  }
  if (Array.isArray(schema.enum)) {
    return schema.enum.map((value) => JSON.stringify(value)).join(" | ");
  }
  switch (schema.type) {
    case "string":
      return "string";
    case "integer":
    case "number":
      return "number";
    case "boolean":
      return "boolean";
    case "array":
      return `${tsType(schema.items, indent)}[]`;
    case "object": {
      const properties = schema.properties ?? {};
      const required = new Set(schema.required ?? []);
      const fields = Object.entries(properties).map(([key, value]) => {
        const optional = required.has(key) ? "" : "?";
        return `${indent}  ${key}${optional}: ${tsType(value, `${indent}  `)};`;
      });
      if (fields.length === 0) {
        return "Record<string, never>";
      }
      return `{\n${fields.join("\n")}\n${indent}}`;
    }
    default:
      throw new Error(`неизвестная схема ${JSON.stringify(schema)}`);
  }
}

/** Комментарий поля из description схемы. */
function docOf(schema, indent) {
  const text = schema?.description;
  return typeof text === "string" ? `${indent}/** ${text} */\n` : "";
}

/** Пути по operationId: имя операции стабильнее, чем адрес. */
function apiPaths() {
  const found = {};
  for (const [path, item] of Object.entries(spec.paths ?? {})) {
    for (const method of Object.values(item ?? {})) {
      if (method && typeof method.operationId === "string") {
        found[method.operationId] = path;
      }
    }
  }
  return found;
}

const names = Object.keys(schemas).sort();
const out = [
  "/**",
  " * Сгенерировано из server/internal/api/openapi.json: npm run api:types.",
  " * Руками не правится: источник правды, Go-структуры ответа.",
  " */",
  "",
];

for (const name of names) {
  const schema = schemas[name];
  if (schema.type !== "object" || !schema.properties) {
    out.push(`export type ${name} = ${tsType(schema)};`, "");
    continue;
  }
  out.push(`export interface ${name} {`);
  const required = new Set(schema.required ?? []);
  for (const [key, value] of Object.entries(schema.properties)) {
    const optional = required.has(key) ? "" : "?";
    out.push(`${docOf(value, "  ")}  ${key}${optional}: ${tsType(value, "  ")};`);
  }
  out.push("}", "");
}

out.push("/** Пути API из спеки: смена адреса на сервере приезжает сюда. */");
out.push("export const paths = {");
for (const [name, path] of Object.entries(apiPaths()).sort()) {
  out.push(`  ${name}: ${JSON.stringify(path)},`);
}
out.push("} as const;", "");

out.push("export interface components {", "  schemas: {");
for (const name of names) {
  out.push(`    ${name}: ${name};`);
}
out.push("  };", "}", "");

writeFileSync(outPath, out.join("\n"));
