/**
 * ServiceLocalityService - Detecção de localidade de serviços federados.
 *
 * Determina se um serviço federado roda no nó atual e gera listas
 * de endpoints com pesos para balanceamento locality-aware.
 *
 * Pesos:
 * - Local: 10 (preferência para o nó atual)
 * - Remoto: 1 (fallback para outros nós)
 */

import { IServiceLocality } from '../core/interfaces/IServiceLocality.js';
import { DiscoveredService } from '../types/docker.js';
import { ServerDefinition } from '../types/federation.js';
import { AppConfig } from '../types/config.js';

/**
 * Peso atribuído a endpoints locais (no mesmo nó).
 */
const LOCAL_WEIGHT = 10;

/**
 * Peso atribuído a endpoints remotos (outros nós).
 */
const REMOTE_WEIGHT = 1;

export class ServiceLocalityService implements IServiceLocality {
    private readonly currentNodeId: string;

    /**
     * Cria uma nova instância do ServiceLocalityService.
     *
     * @param config - Configuração do sidecar, usada para obter o ID do nó atual
     */
    constructor(config: AppConfig) {
        this.currentNodeId = config.node.nodeId;
    }

    /**
     * Verifica se o serviço possui endpoints rodando no nó atual.
     *
     * @param service - Serviço descoberto
     * @returns `true` se pelo menos um endpoint estiver no nó local
     */
    isLocal(service: DiscoveredService): boolean {
        return service.endpoints.some(
            (ep) => ep.nodeId === this.currentNodeId,
        );
    }

    /**
     * Retorna os endpoints locais como definições de servidor Traefik.
     *
     * @param service - Serviço descoberto
     * @returns Lista de servidores para endpoints no nó local
     */
    getLocalEndpoints(service: DiscoveredService): ServerDefinition[] {
        return service.endpoints
            .filter((ep) => ep.nodeId === this.currentNodeId)
            .map((ep) => ({
                url: `http://${ep.nodeIp}:${this.getServicePort(service)}`,
            }));
    }

    /**
     * Retorna os endpoints remotos como definições de servidor Traefik.
     *
     * @param service - Serviço descoberto
     * @returns Lista de servidores para endpoints em nós remotos
     */
    getRemoteEndpoints(service: DiscoveredService): ServerDefinition[] {
        return service.endpoints
            .filter((ep) => ep.nodeId !== this.currentNodeId)
            .map((ep) => ({
                url: `http://${ep.nodeIp}:${this.getServicePort(service)}`,
            }));
    }

    /**
     * Retorna servidores com pesos ajustados com base na localidade,
     * para uso em locality-aware routing.
     *
     * Regras de ponderação:
     * - Se o serviço tem apenas 1 réplica local e nenhuma remota,
     *   retorna apenas o servidor local sem peso (rota direta)
     * - Se o serviço roda em múltiplos nós, atribui pesos:
     *   - Local: {@link LOCAL_WEIGHT} (10)
     *   - Remoto: {@link REMOTE_WEIGHT} (1)
     *
     * @param service - Serviço descoberto
     * @returns Lista de servidores com pesos calculados
     */
    getWeightedServers(service: DiscoveredService): ServerDefinition[] {
        const localEndpoints = service.endpoints.filter(
            (ep) => ep.nodeId === this.currentNodeId,
        );
        const remoteEndpoints = service.endpoints.filter(
            (ep) => ep.nodeId !== this.currentNodeId,
        );

        // Se só tem 1 réplica local, retorna apenas ela sem peso
        if (
            localEndpoints.length === 1 &&
            remoteEndpoints.length === 0
        ) {
            return [
                {
                    url: `http://${localEndpoints[0].nodeIp}:${this.getServicePort(service)}`,
                },
            ];
        }

        // Multi-nó: usa pesos
        const servers: ServerDefinition[] = [
            ...localEndpoints.map((ep) => ({
                url: `http://${ep.nodeIp}:${this.getServicePort(service)}`,
                weight: LOCAL_WEIGHT,
            })),
            ...remoteEndpoints.map((ep) => ({
                url: `http://${ep.nodeIp}:${this.getServicePort(service)}`,
                weight: REMOTE_WEIGHT,
            })),
        ];

        return servers;
    }

    /**
     * Extrai a porta do serviço a partir das labels, com fallback para 80.
     *
     * @param service - Serviço descoberto
     * @returns Porta do serviço
     */
    private getServicePort(service: DiscoveredService): number {
        const portStr = service.labels['federation.port'];
        if (portStr) {
            const port = parseInt(portStr, 10);
            if (!isNaN(port) && port > 0 && port < 65536) {
                return port;
            }
        }
        return 80;
    }
}
