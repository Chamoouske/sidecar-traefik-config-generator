/**
 * EventEmitterService - Sistema de eventos internos do sidecar.
 *
 * Implementa IEventEmitter para permitir comunicação desacoplada entre
 * componentes do sidecar através de um padrão publish/subscribe.
 *
 * Suporte a:
 * - Registro e remoção de handlers por evento
 * - Emissão de eventos com argumentos variádicos
 * - Remoção em massa de listeners (por evento ou global)
 */

import { IEventEmitter, EventHandler } from '../core/interfaces/IEventEmitter.js';

export class EventEmitterService implements IEventEmitter {
    /**
     * Mapa de eventos para conjuntos de handlers registrados.
     */
    private handlers = new Map<string, Set<EventHandler>>();

    /**
     * Registra um handler para um evento específico.
     *
     * Múltiplos handlers podem ser registrados para o mesmo evento.
     * Handlers duplicados são ignorados (Set garante unicidade).
     *
     * @param event   - Nome do evento a ser ouvido
     * @param handler - Função a ser chamada quando o evento for emitido
     */
    on(event: string, handler: EventHandler): void {
        let handlersSet = this.handlers.get(event);

        if (!handlersSet) {
            handlersSet = new Set<EventHandler>();
            this.handlers.set(event, handlersSet);
        }

        handlersSet.add(handler);
    }

    /**
     * Remove um handler previamente registrado para um evento.
     *
     * Se o handler não estiver registrado, a operação é ignorada.
     *
     * @param event   - Nome do evento
     * @param handler - Handler a ser removido
     */
    off(event: string, handler: EventHandler): void {
        const handlersSet = this.handlers.get(event);

        if (!handlersSet) {
            return;
        }

        handlersSet.delete(handler);

        // Limpa o Set se ficou vazio para evitar memory leak
        if (handlersSet.size === 0) {
            this.handlers.delete(event);
        }
    }

    /**
     * Emite um evento, chamando todos os handlers registrados.
     *
     * Os handlers são chamados na ordem em que foram registrados.
     * Se nenhum handler estiver registrado para o evento, a operação
     * é ignorada.
     *
     * @param event - Nome do evento a ser emitido
     * @param args  - Argumentos a serem passados para os handlers
     */
    emit(event: string, ...args: unknown[]): void {
        const handlersSet = this.handlers.get(event);

        if (!handlersSet) {
            return;
        }

        for (const handler of handlersSet) {
            try {
                handler(...args);
            } catch (error) {
                // Impede que um handler com erro quebre os demais
                console.error(
                    `[EventEmitter] Erro no handler do evento "${event}":`,
                    error,
                );
            }
        }
    }

    /**
     * Remove todos os handlers de um evento específico, ou todos os
     * handlers de todos os eventos se nenhum evento for especificado.
     *
     * @param event - (Opcional) Nome do evento. Se omitido, remove todos
     *                os handlers de todos os eventos.
     */
    removeAllListeners(event?: string): void {
        if (event) {
            this.handlers.delete(event);
        } else {
            this.handlers.clear();
        }
    }
}
