import { DiscoveredService } from '../../types/index.js';

/**
 * Interface para descoberta de serviços no Docker Swarm.
 */
export interface ISwarmDiscovery {
    /** Descobre todos os serviços federados no Swarm */
    discoverAllServices(): Promise<DiscoveredService[]>;

    /** Retorna o ID do nó atual */
    getCurrentNodeId(): string;
}
