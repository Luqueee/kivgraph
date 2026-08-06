/**
 * Cross-repository inheritance target: consumer-a's `LabeledWidget`
 * `extends` this class through `@ladygraph-fixture/shared`.
 */
export class Widget {
    id;
    constructor(id) {
        this.id = id;
    }
}
