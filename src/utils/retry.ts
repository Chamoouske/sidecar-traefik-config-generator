/**
 * retryWithBackoff - Utility para retry com backoff exponencial.
 *
 * Tenta executar uma função assíncrona até {@link RetryOptions.maxAttempts}
 * vezes, com delay crescente entre tentativas (backoff exponencial).
 *
 * @example
 * ```typescript
 * const result = await retryWithBackoff(
 *   () => apiClient.fetchData(),
 *   { maxAttempts: 3, baseDelayMs: 1000 },
 * );
 * ```
 */

export interface RetryOptions {
    /** Número máximo de tentativas antes de lançar erro */
    maxAttempts: number;
    /** Delay base em ms (a primeira espera será este valor) */
    baseDelayMs: number;
    /** Delay máximo em ms (cap para evitar esperas muito longas). Default: 30000 */
    maxDelayMs?: number;
}

/**
 * Executa uma função assíncrona com retry e backoff exponencial.
 *
 * O delay entre tentativas segue a fórmula:
 * `Math.min(baseDelayMs * 2^(attempt-1), maxDelayMs)`
 *
 * @param fn      - Função assíncrona a ser executada
 * @param options - Opções de retry (máximo de tentativas, delay base, delay máximo)
 * @returns O resultado da função, se bem-sucedida
 * @throws O último erro encontrado se todas as tentativas falharem
 */
export async function retryWithBackoff<T>(
    fn: () => Promise<T>,
    options: RetryOptions,
): Promise<T> {
    let lastError: Error | undefined;

    for (let attempt = 1; attempt <= options.maxAttempts; attempt++) {
        try {
            return await fn();
        } catch (err) {
            lastError = err instanceof Error ? err : new Error(String(err));

            if (attempt === options.maxAttempts) {
                throw lastError;
            }

            const delay = Math.min(
                options.baseDelayMs * Math.pow(2, attempt - 1),
                options.maxDelayMs ?? 30000,
            );

            await new Promise((resolve) => setTimeout(resolve, delay));
        }
    }

    throw lastError ?? new Error('Retry failed');
}
