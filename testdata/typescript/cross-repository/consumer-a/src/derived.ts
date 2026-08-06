import { Widget } from "@luque-fixture/shared";

export class LabeledWidget extends Widget {
  constructor(
    id: string,
    readonly label: string,
  ) {
    super(id);
  }
}
