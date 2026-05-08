import { SwarmNode, SwarmService, SwarmTask } from '../../types/index.js';

/**
 * Interface para o cliente Docker que interage com a API Dockerode.
 */
export interface IDockerClient {
    /** Estabelece conexão com o daemon Docker */
    connect(): Promise<void>;

    /** Desconecta do daemon Docker */
    disconnect(): Promise<void>;

    /** Retorna a lista de todos os nós do Swarm */
    getNodes(): Promise<SwarmNode[]>;

    /** Retorna a lista de todos os serviços do Swarm */
    getServices(): Promise<SwarmService[]>;

    /** Retorna as tasks de um serviço específico */
    getServiceTasks(serviceId: string): Promise<SwarmTask[]>;

    /** Retorna informações de um nó específico */
    getNodeInfo(nodeId: string): Promise<SwarmNode>;

    /** Verifica se o cliente está conectado ao daemon Docker */
    isConnected(): boolean;

    /** Registra um callback para eventos de reconexão */
    onReconnect(callback: () => void): void;
}
