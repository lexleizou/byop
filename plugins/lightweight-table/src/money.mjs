import { TableError } from "./errors.mjs";

const DECIMAL = /^-?(?:0|[1-9]\d*)(?:\.(\d+))?$/;
const INTEGER = /^-?(?:0|[1-9]\d*)$/;

function parseSafeInteger(value, label) {
  const raw = typeof value === "bigint" ? value.toString() : String(value);
  if (!INTEGER.test(raw)) {
    throw new TableError("invalid_amount", `${label} must be an integer number of cents.`);
  }
  const parsed = BigInt(raw);
  if (
    parsed > BigInt(Number.MAX_SAFE_INTEGER) ||
    parsed < BigInt(Number.MIN_SAFE_INTEGER)
  ) {
    throw new TableError("amount_out_of_range", `${label} exceeds the safe integer range.`);
  }
  return Number(parsed);
}

export function centsFromInteger(value, label = "amount") {
  if (value === null || value === undefined) {
    return null;
  }
  if (typeof value === "number" && !Number.isSafeInteger(value)) {
    throw new TableError("invalid_amount", `${label} must be a safe integer number of cents.`);
  }
  return parseSafeInteger(value, label);
}

export function centsFromDecimal(value, label = "amount", scale = 2) {
  if (value === null || value === undefined) {
    return null;
  }
  if (typeof value !== "string") {
    throw new TableError("invalid_amount", `${label} must be a decimal string.`);
  }
  const match = DECIMAL.exec(value);
  if (!match) {
    throw new TableError("invalid_amount", `${label} must be a decimal string.`);
  }
  const fraction = match[1] ?? "";
  if (fraction.length > scale) {
    throw new TableError(
      "invalid_amount",
      `${label} has more than ${scale} decimal places.`,
    );
  }
  const negative = value.startsWith("-");
  const unsigned = negative ? value.slice(1) : value;
  const [whole, rawFraction = ""] = unsigned.split(".");
  const padded = rawFraction.padEnd(scale, "0");
  const magnitude = BigInt(whole) * 10n ** BigInt(scale) + BigInt(padded || "0");
  const scaled = negative ? -magnitude : magnitude;
  return parseSafeInteger(scaled, label);
}

export function decimalFromCents(value, scale = 2) {
  if (value === null || value === undefined) {
    return null;
  }
  const cents = BigInt(centsFromInteger(value));
  const negative = cents < 0n;
  const magnitude = negative ? -cents : cents;
  const divisor = 10n ** BigInt(scale);
  const whole = magnitude / divisor;
  const fraction = (magnitude % divisor).toString().padStart(scale, "0");
  return `${negative ? "-" : ""}${whole}.${fraction}`;
}

export function sumCents(values) {
  let total = 0n;
  for (const value of values) {
    if (value !== null && value !== undefined) {
      total += BigInt(centsFromInteger(value));
    }
  }
  return total.toString();
}

export function decimalFromCentsString(value, scale = 2) {
  const cents = BigInt(value);
  const negative = cents < 0n;
  const magnitude = negative ? -cents : cents;
  const divisor = 10n ** BigInt(scale);
  const whole = magnitude / divisor;
  const fraction = (magnitude % divisor).toString().padStart(scale, "0");
  return `${negative ? "-" : ""}${whole}.${fraction}`;
}
