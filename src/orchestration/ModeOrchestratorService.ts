/**
 * ModeOrchestratorService - Orquestrador mode-aware.
 *
 * Decide quais serviços de geração executar baseado no GENERATION_MODE.
 * Atua como fachada para os orquestradores Global e Local.
 *
 * Modos:
 * - all:    executa geração global + local (concorrentemente)
 * - global: executa apenas geração global (federação + middlewares)
 * - local:  executa apenas geração local (rotas node-specific)
 *
 * Os orquestradores específicos são opcionais, permitindo que o
 * ModeOrchestratorService seja construído mesmo quando apenas um
 * dos modos é relevante para o nó atual.
 */

import { GenerationMode } from '../types/config.js';
import { IGlobalOrchestrator } from '../core/interfaces/IGlobalOrchestrator.js';
import { ILocalOrchestrator } from '../core/interfaces/ILocalOrchestrator.js';
import { ILogger } from '../core/interfaces/ILogger.js';

export class ModeOrchestratorService {
    constructor(
        private readonly mode: GenerationMode,
        private readonly logger: ILogger,
        private readonly globalOrchestrator?: IGlobalOrchestrator,
        private readonly localOrchestrator?: ILocalOrchestrator,
    ) { }

    /**
     * Executa o(s) ciclo(s) de geração de acordo com o modo configurado.
     *
     * Em modo 'all', executa global e local concorrentemente via Promise.all.
     * Em modo 'global' ou 'local', executa apenas o orquestrador correspondente.
     */
    async runGenerationCycle(): Promise<void> {
        this.logger.info('Iniciando ciclo de geração', { mode: this.mode });

        switch (this.mode) {
            case 'all':
                await Promise.all([
                    this.runGlobalGeneration(),
                    this.runLocalGeneration(),
                ]);
                break;

            case 'global':
                await this.runGlobalGeneration();
                break;

            case 'local':
                await this.runLocalGeneration();
                break;

            default: {
                // Exhaustive check - garante que novos modos sejam tratados
                const _exhaustive: never = this.mode;
                this.logger.warn('Modo de geração não reconhecido', {
                    mode: this.mode,
                });
            }
        }

        this.logger.info('Ciclo de geração concluído', { mode: this.mode });
    }

    /**
     * Executa o ciclo de geração global, se o orquestrador estiver disponível.
     */
    private async runGlobalGeneration(): Promise<void> {
        if (!this.globalOrchestrator) {
            this.logger.warn('Orquestrador global não configurado');
            return;
        }

        this.logger.info('Executando geração global');
        try {
            await this.globalOrchestrator.runGenerationCycle();
        } catch (err) {
            this.logger.error('Geração global falhou', {
                error: (err as Error).message,
            });
        }
    }

    /**
     * Executa o ciclo de geração local, se o orquestrador estiver disponível.
     */
    private async runLocalGeneration(): Promise<void> {
        if (!this.localOrchestrator) {
            this.logger.warn('Orquestrador local não configurado');
            return;
        }

        this.logger.info('Executando geração local');
        try {
            await this.localOrchestrator.runGenerationCycle();
        } catch (err) {
            this.logger.error('Geração local falhou', {
                error: (err as Error).message,
            });
        }
    }
}
