/**
 * DockerClientService - Cliente para a API Docker via dockerode.
 *
 * Responsável por toda comunicação com o daemon Docker:
 * - Conexão e reconexão automática com backoff exponencial
 * - Listagem de nós, serviços e tasks do Swarm
 * - Mapeamento de tipos da API Docker para os tipos internos do sidecar
 */

import Docker from 'dockerode';
import { IDockerClient } from '../core/interfaces/IDockerClient.js';
import { SwarmNode, SwarmService, SwarmTask } from '../types/docker.js';
import { ILogger } from '../core/interfaces/ILogger.js';
import { AppConfig } from '../types/config.js';
import { DockerConnectionError } from '../types/errors.js';

/**
 * Número máximo de tentativas de conexão inicial com o Docker.
 */
const MAX_CONNECT_ATTEMPTS = 5;

/**
 * Intervalo base para backoff exponencial (em ms).
 */
const BASE_BACKOFF_MS = 1000;

/**
 * Intervalo de reconexão após desconexão detectada (em ms).
 */
const RECONNECT_INTERVAL_MS = 10_000;

export class DockerClientService implements IDockerClient {
    private docker: Docker;
    private connected = false;
    private reconnectHandlers: Array<() => void> = [];
    private reconnectTimer?: NodeJS.Timeout;

    /**
     * Cria uma nova instância do DockerClientService.
     *
     * @param config - Configuração contendo o caminho do socket Docker
     * @param logger - Logger para registro de eventos
     */
    constructor(
        private readonly config: AppConfig,
        private readonly logger: ILogger,
    ) {
        this.docker = new Docker({
            socketPath: config.docker.socket,
        });
    }

    /**
     * Estabelece conexão com o daemon Docker.
     *
     * Utiliza backoff exponencial para retry:
     * 1s → 2s → 4s → 8s → 16s (5 tentativas no total).
     * Lança {@link DockerConnectionError} se todas as tentativas falharem.
     */
    async connect(): Promise<void> {
        for (let attempt = 1; attempt <= MAX_CONNECT_ATTEMPTS; attempt++) {
            try {
                await this.docker.ping();
                this.connected = true;
                this.logger.info(
                    `Connected to Docker daemon at ${this.config.docker.socket}`,
                );
                return;
            } catch (err) {
                const delay = BASE_BACKOFF_MS * Math.pow(2, attempt - 1);
                this.logger.warn(
                    `Failed to connect to Docker (attempt ${attempt}/${MAX_CONNECT_ATTEMPTS})`,
                    err instanceof Error ? err : new Error(String(err)),
                );

                if (attempt === MAX_CONNECT_ATTEMPTS) {
                    throw new DockerConnectionError(
                        `Could not connect to Docker after ${MAX_CONNECT_ATTEMPTS} attempts`,
                        err instanceof Error ? err : undefined,
                    );
                }

                this.logger.debug(
                    `Retrying in ${delay}ms...`,
                );
                await new Promise((resolve) =>
                    setTimeout(resolve, delay),
                );
            }
        }
    }

    /**
     * Desconecta do daemon Docker e limpa recursos.
     */
    async disconnect(): Promise<void> {
        this.connected = false;
        if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = undefined;
        }
        this.logger.info('Disconnected from Docker daemon');
    }

    /**
     * Retorna a lista de todos os nós do Swarm.
     *
     * Mapeia os objetos retornados pela API Docker para o tipo
     * {@link SwarmNode}, extraindo IP de `Status.Addr` e
     * hostname de `Description.Hostname`.
     *
     * @returns Lista de nós do Swarm
     */
    async getNodes(): Promise<SwarmNode[]> {
        const nodes: any[] = await this.docker.listNodes();
        return nodes.map((node) => ({
            id: node.ID || '',
            hostname: node.Description?.Hostname || 'unknown',
            ip: node.Status?.Addr || '0.0.0.0',
            availability: node.Spec?.Availability || 'unknown',
            status: node.Status?.State || 'unknown',
        }));
    }

    /**
     * Retorna a lista de todos os serviços do Swarm.
     *
     * Mapeia os objetos retornados pela API Docker para o tipo
     * {@link SwarmService}, extraindo labels, portas, imagem e
     * número de réplicas.
     *
     * @returns Lista de serviços do Swarm
     */
    async getServices(): Promise<SwarmService[]> {
        const services: any[] = await this.docker.listServices();
        return services.map((service) => {
            const spec = service.Spec || {};
            const mode = spec.Mode || {};
            let replicas = 0;

            if (mode.Replicated?.Replicas !== undefined) {
                replicas = mode.Replicated.Replicas;
            } else if (mode.Global) {
                // Serviços globais têm 1 réplica por nó
                replicas = 1;
            }

            return {
                id: service.ID || '',
                name: spec.Name || 'unknown',
                labels: (spec.Labels as Record<string, string>) || {},
                ports: spec.EndpointSpec?.Ports
                    ? spec.EndpointSpec.Ports.map(
                        (p: {
                            PublishedPort: number;
                            TargetPort: number;
                        }) => ({
                            published: p.PublishedPort || 0,
                            target: p.TargetPort || 0,
                        }),
                    )
                    : undefined,
                image:
                    (spec.TaskTemplate?.ContainerSpec as { Image?: string })
                        ?.Image || '',
                replicas,
            };
        });
    }

    /**
     * Retorna as tasks de um serviço específico que estão com
     * `DesiredState = 'running'`.
     *
     * @param serviceId - ID do serviço
     * @returns Lista de tasks running do serviço
     */
    async getServiceTasks(serviceId: string): Promise<SwarmTask[]> {
        const tasks: any[] = await this.docker.listTasks({
            filters: { service: [serviceId] },
        });

        return tasks
            .filter((task) => {
                const desiredState: string =
                    task.DesiredState || '';
                return desiredState === 'running';
            })
            .map((task) => ({
                id: task.ID || '',
                nodeId: task.NodeID || '',
                serviceId: task.ServiceID || '',
                status: task.Status?.State || 'unknown',
                desiredState: task.DesiredState || 'unknown',
                slot: task.Slot || 0,
            }));
    }

    /**
     * Retorna informações detalhadas de um nó específico.
     *
     * @param nodeId - ID do nó
     * @returns Informações do nó
     */
    async getNodeInfo(nodeId: string): Promise<SwarmNode> {
        const node = await this.docker.getNode(nodeId).inspect();
        return {
            id: node.ID || '',
            hostname: node.Description?.Hostname || 'unknown',
            ip: node.Status?.Addr || '0.0.0.0',
            availability: node.Spec?.Availability || 'unknown',
            status: node.Status?.State || 'unknown',
        };
    }

    /**
     * Verifica se o cliente está conectado ao daemon Docker.
     *
     * @returns true se conectado, false caso contrário
     */
    isConnected(): boolean {
        return this.connected;
    }

    /**
     * Registra um callback para ser chamado quando a conexão
     * com o Docker for restabelecida após uma desconexão.
     *
     * @param callback - Função a ser chamada na reconexão
     */
    onReconnect(callback: () => void): void {
        this.reconnectHandlers.push(callback);
    }

    /**
     * Lista containers locais filtrados por labels.
     * Usa a API Docker local (disponível em todos os nós, inclusive workers).
     *
     * @param options - Opções de filtragem (ex: labels)
     * @returns Lista de containers encontrados
     */
    async listContainers(options?: { filters?: Record<string, string[]> }): Promise<any[]> {
        const opts: Docker.ContainerListOptions = {};
        if (options?.filters) {
            opts.filters = options.filters;
        }
        return this.docker.listContainers(opts);
    }

    /**
     * Manipula a detecção de desconexão, iniciando o processo
     * de reconexão automática a cada 10 segundos.
     *
     * Quando reconecta, dispara todos os handlers registrados
     * via {@link onReconnect}.
     */
    private async handleDisconnect(): Promise<void> {
        this.connected = false;
        this.logger.warn(
            'Docker connection lost, attempting to reconnect...',
        );

        const attemptReconnect = async (): Promise<void> => {
            try {
                await this.docker.ping();
                this.connected = true;
                this.logger.info(
                    'Docker reconnected successfully',
                );

                for (const handler of this.reconnectHandlers) {
                    try {
                        handler();
                    } catch (err) {
                        this.logger.error(
                            'Error in reconnect handler',
                            err instanceof Error
                                ? err
                                : new Error(String(err)),
                        );
                    }
                }
            } catch {
                this.logger.debug(
                    'Reconnect attempt failed, retrying...',
                );
                this.reconnectTimer = setTimeout(
                    attemptReconnect,
                    RECONNECT_INTERVAL_MS,
                );
            }
        };

        this.reconnectTimer = setTimeout(
            attemptReconnect,
            RECONNECT_INTERVAL_MS,
        );
    }
}
