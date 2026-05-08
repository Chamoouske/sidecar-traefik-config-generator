/**
 * Tipo de função para manipuladores de eventos.
 */
export type EventHandler = (...args: unknown[]) => void;

/**
 * Interface para o sistema de eventos internos do sidecar.
 * Permite comunicação desacoplada entre componentes.
 */
export interface IEventEmitter {
    /** Registra um handler para um evento específico */
    on(event: string, handler: EventHandler): void;

    /** Remove um handler previamente registrado */
    off(event: string, handler: EventHandler): void;

    /** Emite um evento, chamando todos os handlers registrados */
    emit(event: string, ...args: unknown[]): void;

    /** Remove todos os handlers de um evento, ou todos os handlers se nenhum evento for especificado */
    removeAllListeners(event?: string): void;
}
