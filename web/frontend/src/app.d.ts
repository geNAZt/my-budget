// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	interface Response {
		protobuf(): Promise<any>;
		msgpack(): Promise<any>;
	}
	function fetch(
		input: RequestInfo | URL,
		init?: Omit<RequestInit, 'body'> & { body?: any }
	): Promise<Response>;
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}
}

export {};
