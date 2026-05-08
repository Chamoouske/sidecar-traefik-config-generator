/**
 * Interface para o sistema de logging do sidecar.
 */
export interface ILogger {
    /** Log em nível informativo */
    info(msg: string, ...args: unknown[]): void;

    /** Log em nível de aviso */
    warn(msg: string, ...args: unknown[]): void;

    /** Log em nível de erro */
    error(msg: string, ...args: unknown[]): void;

    /** Log em nível de depuração */
    debug(msg: string, ...args: unknown[]): void;

    /** Log em nível fatal */
    fatal(msg: string, ...args: unknown[]): void;

    /**
     * Cria um logger filho com contexto adicional.
     * @param context - Metadados contextuais para o logger filho
     */
    child(context: Record<string, unknown>): ILogger;
}
