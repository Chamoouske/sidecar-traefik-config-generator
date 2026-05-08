/**
 * LocalDiscoveryService - Descoberta de serviços federados no nó local.
 *
 * Diferente do {@link SwarmDiscoveryService}, este serviço:
 * - Usa `listContainers()` em vez de `getServices()` + `getServiceTasks()`
 * - Funciona em todos os nós (inclusive workers) pois usa API local de containers
 * - Projetado para GENERATION_MODE=local
 */

import { ILocalDiscoveryService } from '../core/interfaces/ILocalDiscoveryService.js';
import { IDockerClient } from '../core/interfaces/IDockerClient.js';
import { ILabelParser } from '../core/interfaces/ILabelParser.js';
import {
    DiscoveredService,
    ServiceEndpoint,
} from '../types/docker.js';
import { AppConfig } from '../types/config.js';
import { ILogger } from '../core/interfaces/ILogger.js';

export class LocalDiscoveryService implements ILocalDiscoveryService {
    /**
     * Cria uma nova instância do LocalDiscoveryService.
     *
     * @param dockerClient - Cliente Docker para consulta à API
     * @param labelParser - Parser de labels para identificar serviços federados
     * @param config - Configuração da aplicação (contém nodeId)
     * @param logger - Logger para registro de eventos
     */
    constructor(
        private readonly dockerClient: IDockerClient,
        private readonly labelParser: ILabelParser,
        private readonly config: AppConfig,
        private readonly logger: ILogger,
    ) { }

    /**
     * Descobre serviços federados rodando no nó atual.
     *
     * Fluxo:
     * 1. Lista containers locais filtrados por label `federation.enable=true`
     * 2. Para cada container, extrai service name, node info e porta
     * 3. Agrupa endpoints por service name
     * 4. Retorna apenas serviços com pelo menos um endpoint
     *
     * Usa `listContainers()` da API Docker local (disponível em todos os nós,
     * inclusive workers), diferentemente de `getServices()` que é Swarm-only.
     *
     * @returns Lista de serviços federados com endpoints no nó atual
     */
    async discoverLocalServices(): Promise<DiscoveredService[]> {
        this.logger.debug('Discovering local services via container inspection');

        // Use local Docker API (available on all nodes, including workers)
        // Filter containers by federation label
        const containers = await this.dockerClient.listContainers({
            filters: {
                label: ['federation.enable=true'],
            },
        });

        if (containers.length === 0) {
            this.logger.debug('No local containers with federation labels found');
            return [];
        }

        const localServices = new Map<string, DiscoveredService>();

        for (const container of containers) {
            try {
                const labels = container.Labels ?? {};
                const serviceName = labels['com.docker.swarm.service.name']
                    || container.Names?.[0]?.replace(/^\//, '')
                    || `container-${container.Id.substring(0, 12)}`;

                // Get node info from config
                const nodeId = this.config.node.nodeId;
                const nodeHostname = this.config.node.hostname;
                const nodeIp = this.config.node.ip;

                // Get container port mapping from label or default to 80
                const portStr = labels['federation.port'] || '80';
                const port = parseInt(portStr, 10) || 80;

                const endpoint: ServiceEndpoint = {
                    nodeId,
                    nodeHostname,
                    nodeIp,
                    taskStatus: container.State || 'running',
                    taskId: container.Id,
                };

                if (localServices.has(serviceName)) {
                    localServices.get(serviceName)!.endpoints.push(endpoint);
                } else {
                    localServices.set(serviceName, {
                        serviceName,
                        serviceId: container.Id,
                        labels: labels as Record<string, string>,
                        endpoints: [endpoint],
                    });
                }
            } catch (err) {
                this.logger.warn('Failed to process container', {
                    containerId: container.Id,
                    error: (err as Error).message,
                });
            }
        }

        const services = Array.from(localServices.values());
        this.logger.info(`Discovered ${services.length} local federated services from ${containers.length} containers`);
        return services;
    }

    /**
     * Retorna o ID do nó atual obtido da configuração.
     *
     * @returns ID do nó atual
     */
    getCurrentNodeId(): string {
        return this.config.node.nodeId;
    }
}
