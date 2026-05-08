import { DiscoveredService, ServerDefinition } from '../../types/index.js';

/**
 * Interface para determinar a localidade de serviços e obter
 * endpoints ponderados para balanceamento de carga entre nós locais e remotos.
 */
export interface IServiceLocality {
    /**
     * Verifica se o serviço está rodando no nó local.
     * @param service - Serviço descoberto
     * @returns true se houver endpoints no nó local
     */
    isLocal(service: DiscoveredService): boolean;

    /**
     * Retorna os endpoints locais do serviço.
     * @param service - Serviço descoberto
     * @returns Lista de definições de servidor para endpoints locais
     */
    getLocalEndpoints(service: DiscoveredService): ServerDefinition[];

    /**
     * Retorna os endpoints remotos do serviço.
     * @param service - Serviço descoberto
     * @returns Lista de definições de servidor para endpoints remotos
     */
    getRemoteEndpoints(service: DiscoveredService): ServerDefinition[];

    /**
     * Retorna servidores com pesos ajustados com base na localidade.
     * @param service - Serviço descoberto
     * @returns Lista de definições de servidor com pesos calculados
     */
    getWeightedServers(service: DiscoveredService): ServerDefinition[];
}
