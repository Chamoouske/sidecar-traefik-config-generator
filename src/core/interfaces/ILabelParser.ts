import { LabelConfig } from '../../types/index.js';

/**
 * Interface para parsing das labels do Traefik nos serviços Docker.
 */
export interface ILabelParser {
    /**
     * Analisa as labels de um serviço e retorna a configuração extraída.
     * @param labels - Mapa de chave/valor das labels do serviço
     * @returns Configuração extraída ou null se o serviço não for federado
     */
    parse(labels: Record<string, string>): LabelConfig | null;

    /**
     * Verifica se um serviço possui federação habilitada através de suas labels.
     * @param labels - Mapa de chave/valor das labels do serviço
     * @returns true se a federação estiver habilitada
     */
    isFederationEnabled(labels: Record<string, string>): boolean;
}
