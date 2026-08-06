import { Widget } from "@ladygraph-fixture/shared";

export class LabeledWidget extends Widget {
  constructor(
    id: string,
    readonly label: string,
  ) {
    super(id);
  }
}
