import "core-js/features/promise";
import * as axios from "axios";

export const test = async () => {
  const res = await axios.default({});
  return new Promise((resolve) => {
    resolve(res.data === "test");
  });
};
