/**
 * FederationConfigGeneratorService - Geração de configuração de federação Traefik.
 *
 * Implementa IFederationStrategy para gerar configurações de balanceamento
 * de carga entre nós do Swarm para serviços federados.
 *
 * Formato gerado:
 * ```yaml
 * http:
 *   services:
 *     {serviceName}:
 *       loadBalancer:
 *         passHostHeader: true
 *         servers:
 *           - url: "http://192.168.1.5:3000"
 *         healthCheck:
 *           path: /
 *           interval: 10s
 *         sticky:
 *           cookie: {}
 *       circuitBreaker:
 *         expression: "NetworkErrorRatio() > 0.30"
 * ```
 */

import { IFederationStrategy } from '../core/interfaces/IFederationStrategy.js';
import { DiscoveredService } from '../types/docker.js';
import {
    FederationConfigOutput,
    ServiceOutput,
    LoadBalancerConfig,
    ServerDefinition,
} from '../types/federation.js';
import { LabelConfig } from '../types/config.js';
import { ILogger } from '../core/interfaces/ILogger.js';
import { IServiceLocality } from '../core/interfaces/IServiceLocality.js';
import { ILabelParser } from '../core/interfaces/ILabelParser.js';

export class FederationConfigGeneratorService implements IFederationStrategy {
    constructor(
        private readonly serviceLocality: IServiceLocality,
        private readonly labelParser: ILabelParser,
        private readonly logger: ILogger,
    ) { }

    /**
     * Verifica se esta estratégia pode processar o serviço.
     *
     * Um serviço é elegível para federação se:
     * - Possui labels de federação válidas (parse retorna não-nulo)
     *
     * @param service - Serviço descoberto a ser avaliado
     * @returns `true` se o serviço tiver configuração de federação válida
     */
    canHandle(service: DiscoveredService): boolean {
        const config = this.labelParser.parse(service.labels);
        return config !== null;
    }

    /**
     * Gera a configuração de federação para um serviço descoberto.
     *
     * O processo de geração:
     * 1. Extrai a configuração de labels do serviço
     * 2. Obtém servidores ponderados via serviceLocality
     * 3. Constrói o LoadBalancerConfig com servidores, health check e sticky session
     * 4. Adiciona circuit breaker se configurado
     * 5. Monta o FederationConfigOutput completo
     *
     * @param service - Serviço descoberto para gerar configuração
     * @returns Configuração de federação Traefik
     */
    async generate(service: DiscoveredService): Promise<FederationConfigOutput> {
        const labelConfig = this.labelParser.parse(service.labels);

        if (!labelConfig) {
            this.logger.warn('Serviço sem configuração de federação válida', {
                serviceName: service.serviceName,
            });
            return {
                http: {
                    services: {},
                },
            };
        }

        this.logger.debug('Gerando configuração de federação', {
            serviceName: service.serviceName,
            host: labelConfig.host,
            port: labelConfig.port,
        });

        // Obtém servidores com pesos ajustados por localidade
        const servers = this.serviceLocality.getWeightedServers(service);

        // Constrói a configuração do load balancer
        const loadBalancer = this.buildLoadBalancerConfig(
            servers,
            labelConfig,
        );

        // Monta o service output
        const serviceOutput: ServiceOutput = {
            loadBalancer,
        };

        // Adiciona circuit breaker se configurado
        if (labelConfig.circuitBreaker) {
            serviceOutput.circuitBreaker = {
                expression: 'NetworkErrorRatio() > 0.30',
            };
            this.logger.debug('Circuit breaker adicionado', {
                serviceName: service.serviceName,
            });
        }

        const output: FederationConfigOutput = {
            http: {
                services: {
                    [service.serviceName]: serviceOutput,
                },
            },
        };

        this.logger.info('Configuração de federação gerada', {
            serviceName: service.serviceName,
            serverCount: servers.length,
        });

        return output;
    }

    /**
     * Constrói a configuração do load balancer Traefik.
     *
     * @param servers   - Lista de definições de servidor com pesos
     * @param labelConfig - Configuração extraída das labels do serviço
     * @returns Configuração completa do load balancer
     */
    private buildLoadBalancerConfig(
        servers: ServerDefinition[],
        labelConfig: LabelConfig,
    ): LoadBalancerConfig {
        const loadBalancer: LoadBalancerConfig = {
            passHostHeader: true,
            servers,
        };

        // Adiciona health check se configurado
        if (labelConfig.healthCheckPath) {
            loadBalancer.healthCheck = {
                path: labelConfig.healthCheckPath,
                interval: labelConfig.healthCheckInterval || '10s',
            };
            this.logger.debug('Health check configurado no load balancer', {
                path: labelConfig.healthCheckPath,
                interval: labelConfig.healthCheckInterval,
            });
        }

        // Adiciona sticky session se configurado
        if (labelConfig.sticky) {
            loadBalancer.sticky = {
                cookie: {},
            };
            this.logger.debug('Sticky session configurado no load balancer');
        }

        return loadBalancer;
    }
}
