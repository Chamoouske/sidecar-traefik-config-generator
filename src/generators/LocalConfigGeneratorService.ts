/**
 * LocalConfigGeneratorService - Geração de configuração Traefik para serviços locais.
 *
 * Gera configurações de serviço e router Traefik para serviços que rodam
 * no nó atual do Swarm, usando resolução DNS interna do Docker.
 *
 * Formato gerado:
 * ```yaml
 * http:
 *   services:
 *     {serviceName}-local:
 *       loadBalancer:
 *         servers:
 *           - url: "http://{serviceName}:{port}"
 *   routers:
 *     {serviceName}-local:
 *       rule: "Host(`{host}`) && Headers(`X-Federated`, `true`)"
 *       service: {serviceName}-local
 *       entryPoints:
 *         - websecure
 *       middlewares:
 *         - {serviceName}-retry
 * ```
 */

import { DiscoveredService } from '../types/docker.js';
import {
    LocalConfigOutput,
    ServiceOutput,
    RouterOutput,
    LoadBalancerConfig,
} from '../types/federation.js';
import { LabelConfig } from '../types/config.js';
import { ILogger } from '../core/interfaces/ILogger.js';
import { IServiceLocality } from '../core/interfaces/IServiceLocality.js';
import { ILocalConfigGenerator } from '../core/interfaces/ILocalConfigGenerator.js';
import { AppConfig } from '../types/config.js';
import { ILabelParser } from '../core/interfaces/ILabelParser.js';

export class LocalConfigGeneratorService implements ILocalConfigGenerator {
    constructor(
        private readonly serviceLocality: IServiceLocality,
        private readonly labelParser: ILabelParser,
        private readonly config: AppConfig,
        private readonly logger: ILogger,
    ) { }

    /**
     * Verifica se o serviço pode ter configuração local gerada.
     *
     * Um serviço gera configuração local apenas se ele roda no nó atual.
     *
     * @param service - Serviço descoberto
     * @returns `true` se houver endpoints no nó local
     */
    canGenerate(service: DiscoveredService): boolean {
        return this.serviceLocality.isLocal(service);
    }

    /**
     * Gera a configuração local Traefik para um serviço.
     *
     * Etapas:
     * 1. Verifica se o serviço é local; se não for, retorna null
     * 2. Extrai labels do serviço via label parser
     * 3. Constrói serviço local apontando para `http://{serviceName}:{port}`
     * 4. Constrói router com regra Host + Header de federação
     * 5. Adiciona middlewares se configurados (retry, circuit breaker)
     *
     * @param service - Serviço descoberto
     * @returns Configuração local ou `null` se o serviço não for local
     */
    async generate(service: DiscoveredService): Promise<LocalConfigOutput | null> {
        if (!this.canGenerate(service)) {
            this.logger.debug('Serviço não é local, pulando geração', {
                serviceName: service.serviceName,
            });
            return null;
        }

        const labelConfig = this.labelParser.parse(service.labels);

        if (!labelConfig) {
            this.logger.warn('Serviço local sem labels de federação válidas', {
                serviceName: service.serviceName,
            });
            return null;
        }

        this.logger.debug('Gerando configuração local', {
            serviceName: service.serviceName,
            host: labelConfig.host,
            port: labelConfig.port,
        });

        // Constrói o serviço local apontando para Docker DNS interno
        const localServiceName = `${service.serviceName}-local`;
        const loadBalancer: LoadBalancerConfig = {
            passHostHeader: true,
            servers: [
                {
                    url: `http://${service.serviceName}:${labelConfig.port}`,
                },
            ],
        };

        // Adiciona health check se configurado
        if (labelConfig.healthCheckPath) {
            loadBalancer.healthCheck = {
                path: labelConfig.healthCheckPath,
                interval: labelConfig.healthCheckInterval || '10s',
            };
        }

        const serviceOutput: ServiceOutput = {
            loadBalancer,
        };

        // Constrói o router com regra de federação
        const middlewares: string[] = [];

        if (labelConfig.retryAttempts && labelConfig.retryAttempts > 0) {
            middlewares.push(`${service.serviceName}-retry`);
        }

        if (labelConfig.circuitBreaker) {
            middlewares.push(`${service.serviceName}-cb`);
        }

        const router: RouterOutput = {
            rule: `Host(\`${labelConfig.host}\`) && Headers(\`${this.config.federation.headerName}\`, \`${this.config.federation.headerValue}\`)`,
            service: localServiceName,
            entryPoints: ['websecure'],
        };

        if (middlewares.length > 0) {
            router.middlewares = middlewares;
        }

        const output: LocalConfigOutput = {
            http: {
                services: {
                    [localServiceName]: serviceOutput,
                },
                routers: {
                    [localServiceName]: router,
                },
            },
        };

        this.logger.info('Configuração local gerada', {
            serviceName: service.serviceName,
            localServiceName,
            middlewares: middlewares.length,
        });

        return output;
    }
}
