/**
 * PinoLogger - Implementação do sistema de logging do sidecar usando Pino.
 *
 * Fornece logging estruturado com suporte a:
 * - Níveis de log (debug, info, warn, error, fatal)
 * - Loggers filhos com contexto adicional
 * - Saída pretty-printed para desenvolvimento
 * - Saída JSON para produção
 */

import pino from 'pino';
import { ILogger } from '../core/interfaces/ILogger.js';
import { AppConfig } from '../types/config.js';

/**
 * Logger filho que encapsula um child logger do Pino.
 * Usado internamente pelo {@link PinoLogger.child} para criar
 * loggers com contexto adicional sem propagar o pai.
 */
class PinoLoggerChild implements ILogger {
    constructor(private readonly logger: pino.Logger) { }

    /** @inheritdoc */
    info(msg: string, ...args: unknown[]): void {
        this.logger.info(args, msg);
    }

    /** @inheritdoc */
    warn(msg: string, ...args: unknown[]): void {
        this.logger.warn(args, msg);
    }

    /** @inheritdoc */
    error(msg: string, ...args: unknown[]): void {
        this.logger.error(args, msg);
    }

    /** @inheritdoc */
    debug(msg: string, ...args: unknown[]): void {
        this.logger.debug(args, msg);
    }

    /** @inheritdoc */
    fatal(msg: string, ...args: unknown[]): void {
        this.logger.fatal(args, msg);
    }

    /** @inheritdoc */
    child(context: Record<string, unknown>): ILogger {
        return new PinoLoggerChild(this.logger.child(context));
    }
}

/**
 * Implementação concreta do logger usando a biblioteca Pino.
 *
 * @example
 * ```typescript
 * const logger = new PinoLogger(config);
 * logger.info('Service started');
 * logger.error('Connection failed', err);
 *
 * const child = logger.child({ service: 'api' });
 * child.info('Request received'); // inclui { service: 'api' } no contexto
 * ```
 */
export class PinoLogger implements ILogger {
    private logger: pino.Logger;

    /**
     * Cria uma nova instância do PinoLogger.
     *
     * @param config - Configuração do sidecar, usada para extrair
     *                 nível de log e formato (pretty vs JSON)
     */
    constructor(config: AppConfig) {
        this.logger = pino({
            level: config.logging.level,
            transport: config.logging.pretty
                ? {
                    target: 'pino-pretty',
                    options: {
                        colorize: true,
                        translateTime: 'SYS:standard',
                    },
                }
                : undefined,
            name: 'sidecar',
        });
    }

    /** @inheritdoc */
    info(msg: string, ...args: unknown[]): void {
        this.logger.info(args, msg);
    }

    /** @inheritdoc */
    warn(msg: string, ...args: unknown[]): void {
        this.logger.warn(args, msg);
    }

    /** @inheritdoc */
    error(msg: string, ...args: unknown[]): void {
        this.logger.error(args, msg);
    }

    /** @inheritdoc */
    debug(msg: string, ...args: unknown[]): void {
        this.logger.debug(args, msg);
    }

    /** @inheritdoc */
    fatal(msg: string, ...args: unknown[]): void {
        this.logger.fatal(args, msg);
    }

    /**
     * Cria um logger filho com contexto adicional.
     *
     * @param context - Metadados contextuais que serão incluídos
     *                  em todas as mensagens do logger filho
     * @returns Uma nova instância de {@link ILogger} com o contexto mesclado
     */
    child(context: Record<string, unknown>): ILogger {
        return new PinoLoggerChild(this.logger.child(context));
    }
}
