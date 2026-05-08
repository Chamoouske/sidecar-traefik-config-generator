/**
 * SwarmDiscoveryService - Descoberta de serviços federados no Docker Swarm.
 *
 * Orquestra o fluxo de descoberta:
 * 1. Lista todos os serviços do Swarm via {@link IDockerClient}
 * 2. Filtra apenas serviços federados via {@link ILabelParser}
 * 3. Para cada serviço federado, busca as tasks running
 * 4. Para cada task, obtém informações do nó onde está rodando
 * 5. Monta a lista de {@link DiscoveredService} com endpoints
 */

import { ISwarmDiscovery } from '../core/interfaces/ISwarmDiscovery.js';

import { IDockerClient } from '../core/interfaces/IDockerClient.js';
import { ILabelParser } from '../core/interfaces/ILabelParser.js';
import {
    DiscoveredService,
    ServiceEndpoint,
} from '../types/docker.js';
import { AppConfig } from '../types/config.js';
import { ILogger } from '../core/interfaces/ILogger.js';

export class SwarmDiscoveryService
    implements ISwarmDiscovery {
    /**
     * Cria uma nova instância do SwarmDiscoveryService.
     *
     * @param dockerClient - Cliente Docker para consulta à API
     * @param labelParser - Parser de labels para identificar serviços federados
     * @param logger - Logger para registro de eventos
     * @param config - Configuração da aplicação (opcional, para nodeId)
     */
    constructor(
        private readonly dockerClient: IDockerClient,
        private readonly labelParser: ILabelParser,
        private readonly logger: ILogger,
        private readonly config?: AppConfig,
    ) { }

    /**
     * Descobre todos os serviços federados no Swarm.
     *
     * Fluxo:
     * 1. Busca todos os serviços com {@link IDockerClient.getServices}
     * 2. Filtra apenas serviços com `federation.enable=true`
     * 3. Para cada serviço, busca tasks running
     * 4. Para cada task, busca informações do nó
     *    (falhas em nós individuais não interrompem o processo)
     * 5. Monta lista de {@link DiscoveredService} com endpoints
     *
     * @returns Lista de serviços federados descobertos
     */
    async discoverAllServices(): Promise<DiscoveredService[]> {
        this.logger.debug('Starting discovery of all federated services');

        const services = await this.dockerClient.getServices();
        const federatedServices: DiscoveredService[] = [];

        for (const service of services) {
            if (
                !this.labelParser.isFederationEnabled(service.labels)
            ) {
                continue;
            }

            this.logger.debug(
                `Processing federated service: ${service.name} (${service.id})`,
            );

            const tasks =
                await this.dockerClient.getServiceTasks(service.id);
            const endpoints: ServiceEndpoint[] = [];

            for (const task of tasks) {
                try {
                    const node =
                        await this.dockerClient.getNodeInfo(
                            task.nodeId,
                        );
                    endpoints.push({
                        nodeId: task.nodeId,
                        nodeHostname: node.hostname,
                        nodeIp: node.ip,
                        taskStatus: task.status,
                        taskId: task.id,
                    });
                } catch (err) {
                    this.logger.warn(
                        `Failed to get node info for task ${task.id} ` +
                        `on service ${service.name}`,
                        err instanceof Error
                            ? err
                            : new Error(String(err)),
                    );
                }
            }

            federatedServices.push({
                serviceName: service.name,
                serviceId: service.id,
                labels: service.labels,
                endpoints,
            });
        }

        const totalEndpoints = federatedServices.reduce(
            (sum, s) => sum + s.endpoints.length,
            0,
        );

        this.logger.info(
            `Discovery completed: ${federatedServices.length} federated ` +
            `services, ${totalEndpoints} total endpoints`,
        );

        return federatedServices;
    }

    /**
     * Retorna o ID do nó atual.
     *
     * Obtido de `config.node.nodeId` quando disponível, com fallback
     * para `NODE_ID` do ambiente ou 'unknown'.
     *
     * @returns ID do nó atual
     */
    getCurrentNodeId(): string {
        return this.config?.node.nodeId ?? process.env.NODE_ID ?? 'unknown';
    }
}
