export class TableError extends Error {
  constructor(code, message, details = undefined) {
    super(message);
    this.name = "TableError";
    this.code = code;
    this.details = details;
  }
}

export function asTableError(error) {
  if (error instanceof TableError) {
    return error;
  }
  const message = error instanceof Error ? error.message : String(error);
  if (/database is locked|SQLITE_BUSY/i.test(message)) {
    return new TableError("busy", "The database stayed busy for the bounded wait.");
  }
  return new TableError("internal_error", message);
}
