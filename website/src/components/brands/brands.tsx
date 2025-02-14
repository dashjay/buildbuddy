import React from "react";
import styles from "./brands.module.css";
import Image from "@theme/IdealImage";

function Component() {
  return (
    <div className={styles.brands}>
      <a target="_blank" href="https://www.ycombinator.com/companies?query=buildbuddy">
        <Image
          alt="Y Combinator Logo"
          width="236px"
          className={styles.image}
          img={require("../../../static/img/yc_monochrome.png")}
        />
      </a>
      <a target="_blank" href="https://cloud.google.com/partners">
        <Image
          alt="Google Cloud Partner Logo"
          width="236px"
          className={styles.image}
          img={require("../../../static/img/gcp_monochrome.png")}
        />
      </a>
      <a
        target="_blank"
        href="https://techcrunch.com/2020/12/01/yc-backed-buildbuddy-raises-3-15m-to-help-developers-build-software-more-quickly/">
        <Image
          alt="Techcrunch Logo"
          width="236px"
          className={styles.image}
          img={require("../../../static/img/tc_monochrome.png")}
        />
      </a>
    </div>
  );
}

export default Component;
