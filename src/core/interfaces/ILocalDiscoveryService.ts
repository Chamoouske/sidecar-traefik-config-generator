import { DiscoveredService } from '../../types/docker.js';
import { ILogger } from './ILogger.js';

/**
 * Interface for local-only service discovery.
 * Used in GENERATION_MODE=local where Swarm cluster APIs are unavailable.
 */
export interface ILocalDiscoveryService {
    /**
     * Discover services running on the local node that have federation labels.
     * Uses local Docker API (containers) rather than Swarm APIs.
     */
    discoverLocalServices(): Promise<DiscoveredService[]>;

    /**
     * Get the current node's identifier.
     */
    getCurrentNodeId(): string;
}
