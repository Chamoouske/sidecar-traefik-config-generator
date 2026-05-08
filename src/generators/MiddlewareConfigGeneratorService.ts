/**
 * MiddlewareConfigGeneratorService - Geração de middlewares Traefik.
 *
 * Implementa IMiddlewareGenerator para gerar configurações de retry
 * e circuit breaker baseadas nas labels dos serviços federados.
 *
 * Formato gerado:
 * ```yaml
 * http:
 *   middlewares:
 *     {serviceName}-retry:
 *       retry:
 *         attempts: 3
 *         initialInterval: 100ms
 *     {serviceName}-cb:
 *       circuitBreaker:
 *         expression: "NetworkErrorRatio() > 0.30"
 * ```
 */

import { IMiddlewareGenerator } from '../core/interfaces/IMiddlewareGenerator.js';
import { DiscoveredService } from '../types/docker.js';
import {
    MiddlewareConfigOutput,
    MiddlewareOutput,
} from '../types/federation.js';
import { ILogger } from '../core/interfaces/ILogger.js';
import { ILabelParser } from '../core/interfaces/ILabelParser.js';

export class MiddlewareConfigGeneratorService implements IMiddlewareGenerator {
    constructor(
        private readonly labelParser: ILabelParser,
        private readonly logger: ILogger,
        private readonly circuitBreakerThreshold: number = 0.30,
    ) { }

    /**
     * Gera configuração de middlewares para um serviço descoberto.
     *
     * Analisa as labels do serviço e gera middlewares baseados em:
     * - `retryAttempts`: se > 0, gera middleware de retry
     * - `circuitBreaker`: se true, gera middleware de circuit breaker
     *
     * @param service - Serviço descoberto
     * @returns Configuração de middlewares ou `null` se nenhum middleware for necessário
     */
    async generate(service: DiscoveredService): Promise<MiddlewareConfigOutput | null> {
        const labelConfig = this.labelParser.parse(service.labels);

        if (!labelConfig) {
            this.logger.debug('Serviço sem labels de federação, pulando middlewares', {
                serviceName: service.serviceName,
            });
            return null;
        }

        const hasRetry = (labelConfig.retryAttempts ?? 0) > 0;
        const hasCircuitBreaker = labelConfig.circuitBreaker === true;

        if (!hasRetry && !hasCircuitBreaker) {
            this.logger.debug('Serviço sem middlewares configurados', {
                serviceName: service.serviceName,
            });
            return null;
        }

        this.logger.debug('Gerando middlewares', {
            serviceName: service.serviceName,
            hasRetry,
            hasCircuitBreaker,
        });

        const middlewares: Record<string, MiddlewareOutput> = {};

        // Middleware de retry
        if (hasRetry) {
            const retryName = `${service.serviceName}-retry`;
            middlewares[retryName] = {
                retry: {
                    attempts: labelConfig.retryAttempts!,
                    initialInterval: labelConfig.retryInterval || '100ms',
                },
            };
            this.logger.debug('Middleware de retry configurado', {
                serviceName: service.serviceName,
                attempts: labelConfig.retryAttempts,
                interval: labelConfig.retryInterval,
            });
        }

        // Middleware de circuit breaker
        if (hasCircuitBreaker) {
            const cbName = `${service.serviceName}-cb`;
            middlewares[cbName] = {
                circuitBreaker: {
                    expression: `NetworkErrorRatio() > ${this.circuitBreakerThreshold}`,
                },
            };
            this.logger.debug('Middleware de circuit breaker configurado', {
                serviceName: service.serviceName,
            });
        }

        const output: MiddlewareConfigOutput = {
            http: {
                middlewares,
            },
        };

        this.logger.info('Middlewares gerados com sucesso', {
            serviceName: service.serviceName,
            middlewareCount: Object.keys(middlewares).length,
        });

        return output;
    }
}
