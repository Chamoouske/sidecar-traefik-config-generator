import { AppConfig } from '../../types/index.js';

/**
 * Interface para gerenciamento de configuração do sidecar.
 */
export interface IConfig {
    /** Carrega a configuração a partir de variáveis de ambiente e defaults */
    load(): AppConfig;

    /** Valida a configuração carregada, lançando erro se inválida */
    validate(): void;

    /** Retorna a configuração atualmente carregada */
    get(): AppConfig;
}
