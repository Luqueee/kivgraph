/**
 * Cross-repository inheritance target: consumer-a's `LabeledWidget`
 * `extends` this class through `@luque-fixture/shared`.
 */
export class Widget {
    id;
    constructor(id) {
        this.id = id;
    }
}
